#!/bin/bash
set -e -o pipefail

OUTPUT=$(go run -mod=readonly golang.org/x/tools/cmd/goimports@v0.30.0 -l -d -local github.com/cloudflare/cloudflared $(go list -mod=vendor -f '{{.Dir}}' -a ./... | fgrep -v tunnelrpc))

if [ -n "$OUTPUT" ] ; then
  PAGER=$(which colordiff || echo cat)
  echo
  echo "Code formatting issues found, use 'make fmt' to correct them"
  echo
  echo "$OUTPUT" | $PAGER
  exit 1
fi
