FROM golang:1.12 as builder
WORKDIR /go/src/github.com/cloudflare/cloudflared/
ADD . .
RUN GOOS=linux go build -a -o cloudflared ./cmd/cloudflared

CMD ["./cloudflared"]  