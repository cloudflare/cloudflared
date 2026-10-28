#!/bin/sh

# Pass the path to the executable to check for FIPS compatibility.
exe=$1
build_info=$(go version -m "${exe}")

if ! printf '%s\n' "${build_info}" | grep -q 'GOFIPS140=latest'; then
    echo "${exe}: missing GOFIPS140=latest build setting" >&2
    exit 1
fi
if ! printf '%s\n' "${build_info}" | grep -q 'DefaultGODEBUG=.*fips140=on'; then
    echo "${exe}: FIPS 140 mode is not enabled by default" >&2
    exit 1
fi

echo "${exe} is FIPS-compatible"
