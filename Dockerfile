FROM golang:1.12 as builder
WORKDIR /go/src/github.com/cloudflare/cloudflared/
RUN apt-get update && apt-get install -y --no-install-recommends upx
# Run after `apt-get update` to improve rebuild scenarios
COPY . .
RUN make cloudflared
RUN upx --no-progress cloudflared

FROM gcr.io/distroless/base
COPY --from=builder /go/src/github.com/cloudflare/cloudflared/cloudflared /usr/local/bin/
ENTRYPOINT ["cloudflared", "--no-autoupdate"]
CMD ["version"]
