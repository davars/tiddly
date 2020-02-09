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
COPY --from=build-env /app/tiddly /bin/tiddly

ENTRYPOINT [ "/bin/tiddly" ]
