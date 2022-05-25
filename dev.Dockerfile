FROM golang:1.17.10 as builder
ENV GO111MODULE=on \
    CGO_ENABLED=0
WORKDIR /go/src/github.com/cloudflare/cloudflared/
RUN apt-get update
COPY . .
# compile cloudflared
RUN make cloudflared
RUN cp /go/src/github.com/cloudflare/cloudflared/cloudflared /usr/local/bin/
