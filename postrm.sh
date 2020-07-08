#!/bin/bash
set -eu
rm /usr/local/bin/cloudflared
rm /usr/local/etc/cloudflared/.installedFromPackageManager || true
