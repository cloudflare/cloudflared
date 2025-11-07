ARG CLOUDFLARE_DOCKER_REGISTRY_HOST

FROM ${CLOUDFLARE_DOCKER_REGISTRY_HOST:-registry.cfdata.org}/stash/cf/debian-images/bookworm/main:2025.7.0@sha256:6350da2f7e728dae2c1420f6dafc38e23cacc0b399d3d5b2f40fe48d9c8ff1ca

RUN apt-get update && \
    apt-get upgrade -y && \
    apt-get install --no-install-recommends --allow-downgrades -y \
        build-essential \
        git \
        go-boring=1.24.9-1 \
        libffi-dev \
        procps \
        python3-dev \
        python3-pip \
        python3-setuptools \
        python3-venv \
        # libmsi and libgcab are libraries the wixl binary depends on.
        libmsi-dev \
        libgcab-dev \
        # deb and rpm build tools
        rubygem-fpm \
        rpm \
        # create deb and rpm repository files
        reprepro \
        createrepo-c \
        # gcc for cross architecture compilation in arm
        gcc-aarch64-linux-gnu \
        libc6-dev-arm64-cross && \
    rm -rf /var/lib/apt/lists/* && \
    # Install wixl
    curl -o /usr/local/bin/wixl -L https://pkg.cloudflare.com/binaries/wixl && \
    chmod a+x /usr/local/bin/wixl && \
    mkdir -p opt

WORKDIR /opt
