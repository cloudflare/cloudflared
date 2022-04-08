# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.17.1 as build
ARG TARGETPLATFORM
ARG BUILDPLATFORM

ENV GO111MODULE=on \
    CGO_ENABLED=0 

ENV FIPS=false

WORKDIR /go/src/github.com/cloudflare/cloudflared/

# build with github tags
#ADD https://github.com/cloudflare/cloudflared/archive/refs/tags/2022.4.0.zip

COPY . .

# compile cloudflared
RUN set -e \
    && echo "Running on $BUILDPLATFORM, building for $TARGETPLATFORM" \
    && apt-get update \
    && apt-get install --no-install-recommends -y ruby \
    && ruby docker-env.rb

FROM --platform=$TARGETPLATFORM alpine:edge
COPY --from=build /go/src/github.com/cloudflare/cloudflared/cloudflared /usr/local/bin/cloudflared

RUN set -e \
    && apk add --no-cache ca-certificates nano

WORKDIR /root

# ref: https://developers.cloudflare.com/1.1.1.1/encryption/dns-over-https/dns-over-https-client/
EXPOSE 53/udp
# ref: https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/configuration/ports-and-ips/
EXPOSE 443
EXPOSE 7844

# Don't set entrypoint, user need edit config file
CMD ["/bin/sh"]
