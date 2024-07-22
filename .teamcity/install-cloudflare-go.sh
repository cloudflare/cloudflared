# !/usr/bin/env bash

cd /tmp
git clone -q https://github.com/cloudflare/go
cd go/src
# https://github.com/cloudflare/go/tree/f4334cdc0c3f22a3bfdd7e66f387e3ffc65a5c38 is version go1.22.5-devel-cf
git checkout -q f4334cdc0c3f22a3bfdd7e66f387e3ffc65a5c38
./make.bash
