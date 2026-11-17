ARG CLOUDFLARE_DOCKER_REGISTRY_HOST

FROM ${CLOUDFLARE_DOCKER_REGISTRY_HOST:-registry.cfdata.org}/stash/cf/debian-images/trixie/main:2026.11.1@sha256:5595f370e48c6d099321753e3d827431ae809f0021a05f3b0b28376a53122115
RUN apt-get update && \
  apt-get upgrade -y && \
  apt-get install --no-install-recommends --allow-downgrades -y \
  build-essential \
  git \
  go=1.27.4-3 \
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
