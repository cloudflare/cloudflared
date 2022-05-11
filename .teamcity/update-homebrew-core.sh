#!/bin/bash

set -euo pipefail

if ! VERSION="$(git describe --tags --exact-match 2>/dev/null)" ; then
    echo "Skipping public release for an untagged commit."
    echo "##teamcity[buildStatus status='SUCCESS' text='Skipped due to lack of tag']"
    exit 0
fi

if [[ "${HOMEBREW_GITHUB_API_TOKEN:-}" == "" ]] ; then
    echo "Missing GITHUB_API_TOKEN"
    exit 1
fi

# "install" Homebrew
git clone https://github.com/Homebrew/brew tmp/homebrew
eval "$(tmp/homebrew/bin/brew shellenv)"
brew update --force --quiet
chmod -R go-w "$(brew --prefix)/share/zsh"

git config --global user.name "cloudflare-warp-bot"
git config --global user.email "warp-bot@cloudflare.com"

# bump formula pr
brew bump-formula-pr cloudflared --version="$VERSION" --no-browse
