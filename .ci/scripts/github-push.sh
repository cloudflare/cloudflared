#!/bin/bash
set -e -o pipefail

mkdir -p tmp

echo "$CLOUDFLARED_DEPLOY_SSH_KEY" > tmp/github-deploy-key
chmod 400 tmp/github-deploy-key

ssh-keyscan -t rsa github.com > tmp/github.pub

export GIT_SSH_COMMAND="ssh -o UserKnownHostsFile=$PWD/tmp/github.pub -o IdentitiesOnly=yes -i $PWD/tmp/github-deploy-key"

git remote add github git@github.com:cloudflare/cloudflared.git || true
#git push -u github master
#if TAG="$(git describe --tags --exact-match 2>/dev/null)"; then
#  git push -u github "$TAG"
#fi

git push -u github origin/joaocarlos/TUN-9800-migrate-github-push-to-gitlab
