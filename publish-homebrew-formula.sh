#!/bin/bash

FILENAME=$1
VERSION=$2
TAP_ROOT=$3
URL="https://developers.cloudflare.com/argo-tunnel/dl/cloudflared-${VERSION}-darwin-amd64.tgz"
SHA256=$(sha256sum -b "${FILENAME}" | cut -b1-64)

cd "${TAP_ROOT}" || exit 1
git checkout -f master
git reset --hard origin/master

tee cloudflared.rb <<EOF
class Cloudflared < Formula
  desc 'Argo Tunnel'
  homepage 'https://developers.cloudflare.com/argo-tunnel/'
  url '${URL}'
  sha256 '${SHA256}'
  version '${VERSION}'
  def install
    bin.install 'cloudflared'
  end
end
EOF
git add cloudflared.rb

git config user.name "cloudflare-warp-bot"
git config user.email "warp-bot@cloudflare.com"
git commit -m "Release Argo Tunnel ${VERSION}"
git version
GIT_SSH_COMMAND="ssh -o UserKnownHostsFile=../github_known_hosts" git push -v origin master
