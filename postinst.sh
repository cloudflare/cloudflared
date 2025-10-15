#!/bin/bash
set -eu
ln -sf /usr/bin/cloudflared /usr/local/bin/cloudflared
mkdir -p /usr/local/etc/cloudflared/
touch /usr/local/etc/cloudflared/.installedFromPackageManager || true
