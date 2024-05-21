# !/usr/bin/env bash

cd /tmp
git clone -q https://github.com/cloudflare/go
cd go/src
# https://github.com/cloudflare/go/tree/ec0a014545f180b0c74dfd687698657a9e86e310 is version go1.22.2-devel-cf
git checkout -q ec0a014545f180b0c74dfd687698657a9e86e310
./make.bash