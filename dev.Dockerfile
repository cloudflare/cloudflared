FROM golang:1.22.2 as builder
ENV GO111MODULE=on \
    CGO_ENABLED=0
WORKDIR /go/src/github.com/cloudflare/cloudflared/
RUN apt-get update
COPY . .
RUN .teamcity/install-cloudflare-go.sh
# compile cloudflared
RUN PATH="/tmp/go/bin:$PATH" make cloudflared
RUN cp /go/src/github.com/cloudflare/cloudflared/cloudflared /usr/local/bin/
