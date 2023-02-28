#!/bin/bash

set -euo pipefail

FILENAME="${PWD}/artifacts/cloudflared-darwin-amd64.tgz"
if ! VERSION="$(git describe --tags --exact-match 2>/dev/null)" ; then
    echo "Skipping public release for an untagged commit."
    echo "##teamcity[buildStatus status='SUCCESS' text='Skipped due to lack of tag']"
    exit 0
fi

if [[ ! -f "$FILENAME" ]] ; then
    echo "Missing $FILENAME"
    exit 1
fi

if [[ "${GITHUB_PRIVATE_KEY:-}" == "" ]] ; then
    echo "Missing GITHUB_PRIVATE_KEY"
    exit 1
fi

# upload to s3 bucket for use by Homebrew formula
s3cmd \
    --acl-public --signature-v2 --access_key="$AWS_ACCESS_KEY_ID" --secret_key="$AWS_SECRET_ACCESS_KEY" --host-bucket="%(bucket)s.s3.cfdata.org" \
    put "$FILENAME" "s3://cftunnel-docs/dl/cloudflared-$VERSION-darwin-amd64.tgz"
s3cmd \
    --acl-public --signature-v2 --access_key="$AWS_ACCESS_KEY_ID" --secret_key="$AWS_SECRET_ACCESS_KEY" --host-bucket="%(bucket)s.s3.cfdata.org" \
    cp "s3://cftunnel-docs/dl/cloudflared-$VERSION-darwin-amd64.tgz" "s3://cftunnel-docs/dl/cloudflared-stable-darwin-amd64.tgz"
SHA256=$(sha256sum "$FILENAME" | cut -b1-64)

# set up git (note that UserKnownHostsFile is an absolute path so we can cd wherever)
mkdir -p tmp
ssh-keyscan -t rsa github.com > tmp/github.txt
echo "$GITHUB_PRIVATE_KEY_B64" | base64 --decode > tmp/private.key
chmod 0400 tmp/private.key
export GIT_SSH_COMMAND="ssh -o UserKnownHostsFile=$PWD/tmp/github.txt -i $PWD/tmp/private.key -o IdentitiesOnly=yes"

# clone Homebrew repo into tmp/homebrew-cloudflare
git clone git@github.com:cloudflare/homebrew-cloudflare.git tmp/homebrew-cloudflare    
cd tmp/homebrew-cloudflare
git checkout -f master
git reset --hard origin/master

# modify cloudflared.rb
URL="https://packages.argotunnel.com/dl/cloudflared-$VERSION-darwin-amd64.tgz"
tee cloudflared.rb <<EOF
class Cloudflared < Formula
  desc 'Cloudflare Tunnel'
  homepage 'https://developers.cloudflare.com/cloudflare-one/connections/connect-apps'
  url '$URL'
  sha256 '$SHA256'
  version '$VERSION'
  def install
    bin.install 'cloudflared'
  end
end
EOF

# push cloudflared.rb
git add cloudflared.rb
git diff
git config user.name "cloudflare-warp-bot"
git config user.email "warp-bot@cloudflare.com"
git commit -m "Release Cloudflare Tunnel $VERSION"

git push -v origin master
