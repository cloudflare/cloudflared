# use a builder image for building cloudflare
ARG TARGET_GOOS
ARG TARGET_GOARCH
FROM golang:1.24.11 AS builder
ENV GO111MODULE=on \
  CGO_ENABLED=0 \
  TARGET_GOOS=${TARGET_GOOS} \
  TARGET_GOARCH=${TARGET_GOARCH} \
  # the CONTAINER_BUILD envvar is used set github.com/cloudflare/cloudflared/metrics.Runtime=virtual
  # which changes how cloudflared binds the metrics server
  CONTAINER_BUILD=1


WORKDIR /go/src/github.com/cloudflare/cloudflared/

# copy our sources into the builder image
COPY . .

# compile cloudflared
RUN make cloudflared

# use a distroless base image with glibc
FROM gcr.io/distroless/base-debian13:nonroot
# Enable metrics for healthcheck
ENV TUNNEL_METRICS=127.0.0.1:60123

LABEL org.opencontainers.image.source="https://github.com/cloudflare/cloudflared"

# copy our compiled binary
COPY --from=builder --chown=nonroot /go/src/github.com/cloudflare/cloudflared/cloudflared /usr/local/bin/

# run as nonroot user
# We need to use numeric user id's because Kubernetes doesn't support strings:
# https://github.com/kubernetes/kubernetes/blob/v1.33.2/pkg/kubelet/kuberuntime/security_context_others.go#L49
# The `nonroot` user maps to `65532`, from: https://github.com/GoogleContainerTools/distroless/blob/main/common/variables.bzl#L18
USER 65532:65532

# Check if cloudflared is healthy
HEALTHCHECK --interval=30s --timeout=30s --retries=3 \
  CMD cloudflared tunnel --metrics localhost:60123 ready

# command / entrypoint of container
ENTRYPOINT ["cloudflared", "--no-autoupdate"]
CMD ["version"]
