# !/usr/bin/env bash

cd /tmp
git clone -q https://github.com/cloudflare/go
cd go/src
# https://github.com/cloudflare/go/tree/af19da5605ca11f85776ef7af3384a02a315a52b is version go1.22.5-devel-cf
git checkout -q af19da5605ca11f85776ef7af3384a02a315a52b
./make.bash
