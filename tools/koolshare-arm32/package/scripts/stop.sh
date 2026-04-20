#!/usr/bin/env sh
set -eu

INSTALL_ROOT="${KSROOT:-/koolshare}"
PID_FILE="${INSTALL_ROOT}/var/run/cloudflared.pid"

if [ ! -f "${PID_FILE}" ]; then
  echo "cloudflared is not running"
  exit 0
fi

PID="$(cat "${PID_FILE}")"

if ! [ "${PID}" -eq "${PID}" ] 2>/dev/null; then
  echo "invalid pid file content; removing stale pid file"
  rm -f "${PID_FILE}"
  exit 0
fi

if kill -0 "${PID}" >/dev/null 2>&1; then
  CMDLINE_FILE="/proc/${PID}/cmdline"
  CMDLINE=""
  if [ -r "${CMDLINE_FILE}" ]; then
    CMDLINE="$(tr '\000' ' ' < "${CMDLINE_FILE}")"
  fi

  case "${CMDLINE}" in
    *cloudflared*)
      kill "${PID}"
      echo "cloudflared stopped (pid ${PID})"
      ;;
    *)
      echo "stale pid file found (pid ${PID} does not belong to cloudflared)"
      ;;
  esac
else
  echo "stale pid file found"
fi

rm -f "${PID_FILE}"
