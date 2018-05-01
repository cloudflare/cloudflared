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
  must go get -t ./...
else
  BAZEL_VERSION="${BAZEL_VERSION:-0.14.1}"
  case "$TRAVIS_OS_NAME" in
    linux)
      BAZEL_INSTALLER_URL="https://github.com/bazelbuild/bazel/releases/download/${BAZEL_VERSION}/bazel-${BAZEL_VERSION}-installer-linux-x86_64.sh"
      SEDI="sed -i"
      ;;
    osx)
      BAZEL_INSTALLER_URL="https://github.com/bazelbuild/bazel/releases/download/${BAZEL_VERSION}/bazel-${BAZEL_VERSION}-installer-darwin-x86_64.sh"
      SEDI="sed -i ''"
      ;;
    *)
      die "unknown OS $TRAVIS_OS_NAME"
      ;;
  esac
  must curl -fsSLo /tmp/bazel.sh "$BAZEL_INSTALLER_URL"
  must chmod +x /tmp/bazel.sh
  must /tmp/bazel.sh --user
  rm -f /tmp/bazel.sh
  if [[ ! -z "$TRAVIS_GO_VERSION" ]]; then
    must $SEDI -e 's/^go_register_toolchains()/go_register_toolchains(go_version="host")/' WORKSPACE
  fi
  must "$HOME/bin/bazel" --bazelrc=_travis/bazelrc version
  must "$HOME/bin/bazel" --bazelrc=_travis/bazelrc fetch //...
fi
