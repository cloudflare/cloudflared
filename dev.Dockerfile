FROM golang:1.12 as builder
WORKDIR /go/src/github.com/cloudflare/cloudflared/
RUN apt-get update
COPY . .
