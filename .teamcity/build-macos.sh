#!/bin/bash

set -euo pipefail

if [[ "$(uname)" != "Darwin" ]] ; then
    echo "This should be run on macOS"
    exit 1
fi

go version
export GO111MODULE=on

# build 'cloudflared-darwin-amd64.tgz'
mkdir -p artifacts
FILENAME="$(pwd)/artifacts/cloudflared-darwin-amd64.tgz"
export PATH="$PATH:/usr/local/bin"
mkdir -p ../src/github.com/cloudflare/    
cp -r . ../src/github.com/cloudflare/cloudflared
cd ../src/github.com/cloudflare/cloudflared 
GOCACHE="$PWD/../../../../" GOPATH="$PWD/../../../../" CGO_ENABLED=1 make cloudflared
tar czf "$FILENAME" cloudflared
