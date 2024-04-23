# !/usr/bin/env bash

cd /tmp
git clone -q https://github.com/cloudflare/go
cd go/src
# https://github.com/cloudflare/go/tree/34129e47042e214121b6bbff0ded4712debed18e is version go1.21.5-devel-cf
git checkout -q 34129e47042e214121b6bbff0ded4712debed18e
./make.bash