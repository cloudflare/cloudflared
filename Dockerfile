# use a builder image for building cloudflare
ARG TARGET_GOOS
ARG TARGET_GOARCH
FROM golang:1.17.1 as builder
ARG TARGET_GOOS
ARG TARGET_GOARCH
ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    TARGET_OS=${TARGET_GOOS} \
    TARGET_ARCH=${TARGET_GOARCH}
    
LABEL org.opencontainers.image.source="https://github.com/cloudflare/cloudflared"

WORKDIR /go/src/github.com/cloudflare/cloudflared/

# copy our sources into the builder image
COPY . .

# compile cloudflared
RUN make cloudflared

# use a distroless base image with glibc
FROM gcr.io/distroless/base-debian10:nonroot

# copy our compiled binary
COPY --from=builder --chown=nonroot /go/src/github.com/cloudflare/cloudflared/cloudflared /usr/local/bin/

# run as non-privileged user
USER nonroot

# command / entrypoint of container
ENTRYPOINT ["cloudflared", "--no-autoupdate"]
CMD ["version"]
