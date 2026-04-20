#!/usr/bin/env sh
set -eu

INSTALL_ROOT="${KSROOT:-/koolshare}"
BIN_PATH="${INSTALL_ROOT}/bin/cloudflared"
PID_FILE="${INSTALL_ROOT}/var/run/cloudflared.pid"

if [ -f "${PID_FILE}" ]; then
  kill "$(cat "${PID_FILE}")" >/dev/null 2>&1 || true
  rm -f "${PID_FILE}"
fi

rm -f "${BIN_PATH}"
echo "cloudflared removed from ${BIN_PATH}"
