ARG CLOUDFLARE_DOCKER_REGISTRY_HOST

FROM ${CLOUDFLARE_DOCKER_REGISTRY_HOST:-registry.cfdata.org}/stash/cf/debian-images/trixie/main:2026.1.0@sha256:e32092fd01520f5ae7de1fa6bb5a721720900ebeaa48e98f36f6f86168833cd7
RUN apt-get update && \
  apt-get upgrade -y && \
  apt-get install --no-install-recommends --allow-downgrades -y \
  build-essential \
  git \
  go-boring=1.24.13-1 \
  libffi-dev \
  procps \
  python3-dev \
  python3-pip \
  python3-setuptools \
  python3-venv \
  # tool to create msi packages
  wixl \
  # install ruby and rpm which are required to install fpm package builder
  rpm \
  ruby \
  ruby-dev \
  rubygems \
  # create deb and rpm repository files
  reprepro \
  createrepo-c \
  # gcc for cross architecture compilation in arm
  gcc-aarch64-linux-gnu \
  libc6-dev-arm64-cross && \
  rm -rf /var/lib/apt/lists/* && \
  # Install fpm gem
  gem install fpm --no-document && \
  # Initialize rpm repository, SQL Lite DB
  mkdir -p /var/lib/rpm && \
  rpm --initdb && \
  chmod -R 777 /var/lib/rpm && \
  # Create work directory
  mkdir -p opt

WORKDIR /opt
