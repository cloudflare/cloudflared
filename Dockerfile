# use a builder image for building cloudflare
ARG TARGET_GOOS
ARG TARGET_GOARCH
FROM golang:1.22.2 as builder
ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    TARGET_GOOS=${TARGET_GOOS} \
    TARGET_GOARCH=${TARGET_GOARCH}

WORKDIR /go/src/github.com/cloudflare/cloudflared/

# copy our sources into the builder image
COPY . .

RUN .teamcity/install-cloudflare-go.sh

# compile cloudflared
RUN PATH="/tmp/go/bin:$PATH" make cloudflared

# use a distroless base image with glibc
FROM gcr.io/distroless/base-debian11:nonroot

LABEL org.opencontainers.image.source="https://github.com/cloudflare/cloudflared"

# copy our compiled binary
COPY --from=builder --chown=nonroot /go/src/github.com/cloudflare/cloudflared/cloudflared /usr/local/bin/

# run as non-privileged user
USER nonroot

# set health-check
HEALTHCHECK &{["CMD-SHELL" "curl --fail http://127.0.0.1:80/ || exit 1"] "1m0s" "40s" "0s" '\n'}

# command / entrypoint of container
ENTRYPOINT ["cloudflared", "--no-autoupdate"]
CMD ["version"]
