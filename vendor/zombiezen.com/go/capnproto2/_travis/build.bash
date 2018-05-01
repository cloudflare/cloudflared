#!/bin/bash

must() {
  echo "$@" 1>&2
  "$@" || die "FAIL"
}

die() {
  echo "$@" 1>&2
  exit 1
}

if [[ -z "$USE_BAZEL" || "$USE_BAZEL" -eq "0" ]]; then
  must go test -v ./...
else
  # On Travis, this will use "$HOME/bin/bazel", but don't assume this
  # for local testing of the CI script.
  must bazel --bazelrc=_travis/bazelrc test //...
fi
