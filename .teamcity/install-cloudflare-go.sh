# !/usr/bin/env bash

cd /tmp
git clone -q https://github.com/cloudflare/go
cd go/src
# https://github.com/cloudflare/go/tree/37bc41c6ff79507200a315b72834fce6ca427a7e is version go1.22.12-devel-cf
git checkout -q 37bc41c6ff79507200a315b72834fce6ca427a7e
./make.bash
