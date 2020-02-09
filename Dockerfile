FROM golang:latest AS build-env
ENV GO111MODULE=on
WORKDIR /app
COPY go.mod /app
COPY go.sum /app
RUN go mod download
COPY . /app
RUN go build -v
RUN ls

FROM debian:latest
ADD https://raw.githubusercontent.com/cloudflare/cfssl_trust/master/ca-bundle.crt /etc/ssl/certs/ca-certificates.crt
RUN chmod a+r /etc/ssl/certs/ca-certificates.crt
RUN mkdir /app
WORKDIR /app
# Volume: /path/to/service-account-key.json:/credentials.json
ENV GOOGLE_APPLICATION_CREDENTIALS=/credentials.json
COPY --from=build-env /app/tiddly /app/tiddly
COPY --from=build-env /app/index.html /app/index.html
ENTRYPOINT [ "/usr/sbin/chroot", "--skip-chdir", "--userspec=nobody", "/", "/app/tiddly" ]
