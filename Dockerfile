# use a builder image for building cloudflare
ARG TARGET_GOOS=linux
ARG TARGET_GOARCH=arm
FROM golang:1.13.3 as builder
ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=${TARGET_GOOS} \
    GOARCH=${TARGET_GOARCH}

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
