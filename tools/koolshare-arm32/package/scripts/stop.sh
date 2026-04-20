#!/usr/bin/env sh
set -eu

INSTALL_ROOT="${KSROOT:-/koolshare}"
PID_FILE="${INSTALL_ROOT}/var/run/cloudflared.pid"

if [ ! -f "${PID_FILE}" ]; then
  echo "cloudflared is not running"
  exit 0
fi

PID="$(cat "${PID_FILE}")"
if kill -0 "${PID}" >/dev/null 2>&1; then
  kill "${PID}"
  echo "cloudflared stopped (pid ${PID})"
else
  echo "stale pid file found"
fi

rm -f "${PID_FILE}"
