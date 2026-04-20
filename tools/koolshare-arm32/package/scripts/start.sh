#!/usr/bin/env sh
set -eu

INSTALL_ROOT="${KSROOT:-/koolshare}"
BIN_PATH="${INSTALL_ROOT}/bin/cloudflared"
CONFIG_PATH="${INSTALL_ROOT}/configs/cloudflared/config.yml"
PID_FILE="${INSTALL_ROOT}/var/run/cloudflared.pid"
LOG_FILE="${INSTALL_ROOT}/var/log/cloudflared.log"

if [ ! -x "${BIN_PATH}" ]; then
  echo "cloudflared not found: ${BIN_PATH}" >&2
  exit 1
fi

if [ ! -f "${CONFIG_PATH}" ]; then
  echo "cloudflared config not found: ${CONFIG_PATH}" >&2
  exit 1
fi

if [ -f "${PID_FILE}" ] && kill -0 "$(cat "${PID_FILE}")" >/dev/null 2>&1; then
  echo "cloudflared already running with pid $(cat "${PID_FILE}")"
  exit 0
fi

nohup "${BIN_PATH}" --config "${CONFIG_PATH}" tunnel run >>"${LOG_FILE}" 2>&1 &
echo "$!" > "${PID_FILE}"
echo "cloudflared started with pid $!"
