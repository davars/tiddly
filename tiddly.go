// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"embed"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"cloud.google.com/go/datastore"
	"google.golang.org/api/iterator"
)

// Re Authentication
//
// With the apparent impending demise of the App Engine Users API, I've converted this version to sit behind an
// authenticating proxy like https://github.com/davars/sohop or https://github.com/pusher/oauth2_proxy.  Set the
// X-Webauth-User header to the authorized user's ID.  In sohop you can add a Headers clause like:
//     "tiddly": {
//      "URL": "http://127.0.0.1:8080",
//      "HealthCheck": "http://127.0.0.1:8080/health",
//      "Auth": true,
//      "Headers": { "X-WEBAUTH-USER":["{{.Session.Values.user}}"] }
//    },
//

var dsClient = func() *datastore.Client {
	project := os.Getenv("GCP_PROJECT")
	if project == "" {
		log.Fatal("must set GCP_PROJECT env var")
	}
	cli, err := datastore.NewClient(context.Background(), project)
	if err != nil {
		log.Fatal(err)
	}
	return cli
}()

func main() {
	r := http.NewServeMux()
	r.HandleFunc("/", root)
	r.HandleFunc("/auth", auth)
	r.HandleFunc("/status", status)
	r.HandleFunc("/recipes/all/tiddlers/", tiddler)
	r.HandleFunc("/recipes/all/tiddlers.json", tiddlerList)
	r.HandleFunc("/bags/bag/tiddlers/", deleteTiddler)

	http.HandleFunc("/health", health)
	http.Handle("/", authCheck(r))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		log.Printf("Defaulting to port %s", port)
	}

	log.Printf("Listening on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func currentUser(r *http.Request) string {
	return r.Header.Get("X-Webauth-User")
}

func authCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !mustBeAdmin(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func mustBeAdmin(w http.ResponseWriter, r *http.Request) bool {
	if currentUser(r) == "" {
		http.Error(w, "permission denied", 403)
		return false
	}
	return true
}

type Tiddler struct {
	Rev  int    `datastore:"Rev,noindex"`
	Meta string `datastore:"Meta,noindex"`
	Text string `datastore:"Text,noindex"`
}

//go:embed index.html
var content embed.FS
var contentFS = http.FileServer(http.FS(content))

func root(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "bad method", 405)
		return
	}
	if r.URL.Path != "/" {
		http.Error(w, "not found", 404)
		return
	}
	contentFS.ServeHTTP(w, r)
}

func health(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "ok")
}

func auth(w http.ResponseWriter, r *http.Request) {
	name := currentUser(r)
	if name == "" {
		name = "GUEST"
	}
	fmt.Fprintf(w, "<html>\nYou are logged in as %s.\n\n<a href=\"/\">Main page</a>.\n", name)
}

func status(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "bad method", 405)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	name := currentUser(r)
	if name == "" {
		name = "GUEST"
	}
	w.Write([]byte(`{"username": "` + name + `", "space": {"recipe": "all"}}`))
}

func tiddlerList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := datastore.NewQuery("Tiddler")
	// Only need Meta, but get no results if we do this.
	if false {
		q = q.Project("Meta")
	}

	it := dsClient.Run(ctx, q)
	var buf bytes.Buffer
	sep := ""
	buf.WriteString("[")
	for {
		var t Tiddler
		_, err := it.Next(&t)
		if err != nil {
			if err == iterator.Done {
				break
			}
			println("ERR", err.Error())
			http.Error(w, err.Error(), 500)
			return
		}
		if len(t.Meta) == 0 {
			continue
		}
		meta := t.Meta

		// Tiddlers containing macros don't take effect until
		// they are loaded. Force them to be loaded by including
		// their bodies in the skinny tiddler list.
		// Might need to expand this to other kinds of tiddlers
		// in the future as we discover them.
		if strings.Contains(meta, `"$:/tags/Macro"`) {
			var js map[string]interface{}
			err := json.Unmarshal([]byte(meta), &js)
			if err != nil {
				continue
			}
			js["text"] = string(t.Text)
			data, err := json.Marshal(js)
			if err != nil {
				continue
			}
			meta = string(data)
		}

		buf.WriteString(sep)
		sep = ","
		buf.WriteString(meta)
	}
	buf.WriteString("]")
	w.Header().Set("Content-Type", "application/json")
	w.Write(buf.Bytes())
}

func tiddler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		getTiddler(w, r)
	case "PUT":
		putTiddler(w, r)
	default:
		http.Error(w, "bad method", 405)
	}
}

func getTiddler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	title := strings.TrimPrefix(r.URL.Path, "/recipes/all/tiddlers/")
	key := datastore.NameKey("Tiddler", title, nil)
	var t Tiddler
	if err := dsClient.Get(ctx, key, &t); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	var js map[string]interface{}
	err := json.Unmarshal([]byte(t.Meta), &js)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	js["text"] = string(t.Text)
	data, err := json.Marshal(js)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func putTiddler(w http.ResponseWriter, r *http.Request) {
	if !mustBeAdmin(w, r) {
		return
	}
	ctx := r.Context()
	title := strings.TrimPrefix(r.URL.Path, "/recipes/all/tiddlers/")
	key := datastore.NameKey("Tiddler", title, nil)
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "cannot read data", 400)
		return
	}
	var js map[string]interface{}
	err = json.Unmarshal(data, &js)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	js["bag"] = "bag"

	rev := 1
	var old Tiddler
	if err := dsClient.Get(ctx, key, &old); err == nil {
		rev = old.Rev + 1
	}
	js["revision"] = rev

	var t Tiddler
	text, ok := js["text"].(string)
	if ok {
		t.Text = text
	}
	delete(js, "text")
	t.Rev = rev
	meta, err := json.Marshal(js)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	t.Meta = string(meta)
	_, err = dsClient.Put(ctx, key, &t)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	key2 := datastore.NameKey("TiddlerHistory", title+"#"+fmt.Sprint(t.Rev), nil)
	if _, err := dsClient.Put(ctx, key2, &t); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	etag := fmt.Sprintf("\"bag/%s/%d:%x\"", url.QueryEscape(title), rev, md5.Sum(data))
	w.Header().Set("Etag", etag)
}

func deleteTiddler(w http.ResponseWriter, r *http.Request) {
	if !mustBeAdmin(w, r) {
		return
	}
	ctx := r.Context()
	if r.Method != "DELETE" {
		http.Error(w, "bad method", 405)
		return
	}
	title := strings.TrimPrefix(r.URL.Path, "/bags/bag/tiddlers/")
	key := datastore.NameKey("Tiddler", title, nil)
	var t Tiddler
	if err := dsClient.Get(ctx, key, &t); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	t.Rev++
	t.Meta = ""
	t.Text = ""
	if _, err := dsClient.Put(ctx, key, &t); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	key2 := datastore.NameKey("TiddlerHistory", title+"#"+fmt.Sprint(t.Rev), nil)
	if _, err := dsClient.Put(ctx, key2, &t); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
}
