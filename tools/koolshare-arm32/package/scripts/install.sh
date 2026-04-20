#!/usr/bin/env sh
set -eu

INSTALL_ROOT="${KSROOT:-/koolshare}"
BIN_DIR="${INSTALL_ROOT}/bin"
CONFIG_DIR="${INSTALL_ROOT}/configs/cloudflared"
RUN_DIR="${INSTALL_ROOT}/var/run"
LOG_DIR="${INSTALL_ROOT}/var/log"

mkdir -p "${BIN_DIR}" "${CONFIG_DIR}" "${RUN_DIR}" "${LOG_DIR}"
install -m 0755 ./bin/cloudflared "${BIN_DIR}/cloudflared"

if [ ! -f "${CONFIG_DIR}/config.yml" ] && [ -f ./config/config.yml.example ]; then
  install -m 0644 ./config/config.yml.example "${CONFIG_DIR}/config.yml"
fi

echo "cloudflared installed to ${BIN_DIR}/cloudflared"
echo "config location: ${CONFIG_DIR}/config.yml"
echo "Use scripts/start.sh to launch tunnel service"
