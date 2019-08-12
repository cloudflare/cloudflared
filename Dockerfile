# use a builder image for building cloudflare
FROM golang:1.12 as builder

# switch to the right gopath directory
WORKDIR /go/src/github.com/cloudflare/cloudflared/

# copy our sources into the builder image
COPY . .

# compile cloudflared
RUN make cloudflared

# ---

# use a distroless base image with glibc
FROM gcr.io/distroless/base

# copy our compiled binary
COPY --from=builder --chown=nonroot /go/src/github.com/cloudflare/cloudflared/cloudflared /usr/local/bin/

# run as non-privileged user
USER nonroot

# command / entrypoint of container
ENTRYPOINT ["cloudflared", "--no-autoupdate"]
CMD ["version"]
