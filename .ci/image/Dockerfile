ARG CLOUDFLARE_DOCKER_REGISTRY_HOST

FROM ${CLOUDFLARE_DOCKER_REGISTRY_HOST:-registry.cfdata.org}/stash/cf/debian-images/trixie/main:2026.1.0@sha256:e32092fd01520f5ae7de1fa6bb5a721720900ebeaa48e98f36f6f86168833cd7
RUN apt-get update && \
    apt-get upgrade -y && \
    apt-get install --no-install-recommends --allow-downgrades -y \
        build-essential \
        git \
        go-boring=1.24.11-1 \
        libffi-dev \
        procps \
        python3-dev \
        python3-pip \
        python3-setuptools \
        python3-venv \
        # tool to create msi packages
        wixl \
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
    mkdir -p opt

WORKDIR /opt
