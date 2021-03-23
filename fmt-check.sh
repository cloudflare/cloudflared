#!/bin/bash

set -e -o pipefail

OUTPUT=$(for d in $(go list -mod=vendor -f '{{.Dir}}' -a ./... | fgrep -v tunnelrpc) ; do goimports -format-only -local github.com/cloudflare/cloudflared -d $d ; done)

if [ -n "$OUTPUT" ] ; then
  PAGER=$(which colordiff || echo cat)
  echo
  echo "Code formatting issues found, use 'goimports -format-only -local github.com/cloudflare/cloudflared' to correct them"
  echo
  echo "$OUTPUT" | $PAGER
  exit 1
fi
