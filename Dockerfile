FROM golang:1.12-alpine as builder

WORKDIR /go/src/github.com/cloudflare/cloudflared/

COPY . .

ENV GO111MODULE on
ENV CGO_ENABLED 0

RUN apk add --no-cache build-base=0.5-r1 git=2.22.0-r0 upx=3.95-r2 \
    && make cloudflared \
    && upx --no-progress cloudflared

FROM scratch

COPY --from=builder /go/src/github.com/cloudflare/cloudflared/cloudflared /usr/local/bin/

ENTRYPOINT ["cloudflared", "--no-autoupdate"]

CMD ["version"]

RUN ["cloudflared", "--version"]
