FROM --platform=$BUILDPLATFORM golang:1.23-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
ARG BINNAME

WORKDIR /src/

COPY go.* /src/
RUN go mod download

COPY . .
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /out/tiddly .

FROM debian:bookworm-slim

ADD https://raw.githubusercontent.com/cloudflare/cfssl_trust/master/ca-bundle.crt /etc/ssl/certs/ca-certificates.crt
RUN chmod a+r /etc/ssl/certs/ca-certificates.crt

COPY --from=build /out/tiddly /usr/local/bin/tiddly
EXPOSE 8080
ENTRYPOINT [ "/usr/sbin/chroot", "--skip-chdir", "--userspec=nobody", "/", "/usr/local/bin/tiddly" ]
