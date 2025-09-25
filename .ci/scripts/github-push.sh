#!/bin/bash
set -e -o pipefail

BRANCH="master"
TMP_PATH="$PWD/tmp"
PRIVATE_KEY_PATH="$TMP_PATH/github-deploy-key"
PUBLIC_KEY_GITHUB_PATH="$TMP_PATH/github.pub"

mkdir -p $TMP_PATH

# Setup Private Key
echo "$CLOUDFLARED_DEPLOY_SSH_KEY" > $PRIVATE_KEY_PATH
chmod 400 $PRIVATE_KEY_PATH

# Download GitHub Public Key for KnownHostsFile
ssh-keyscan -t ed25519 github.com > $PUBLIC_KEY_GITHUB_PATH

# Setup git ssh command with the right configurations
export GIT_SSH_COMMAND="ssh -o UserKnownHostsFile=$PUBLIC_KEY_GITHUB_PATH -o IdentitiesOnly=yes -i $PRIVATE_KEY_PATH"

# Add GitHub as a new remote
git remote add github git@github.com:cloudflare/cloudflared.git || true

# GitLab doesn't pull branch references, instead it creates a new one on each pipeline.
# Therefore, we need to manually fetch the reference to then push it to GitHub.
git fetch origin $BRANCH:$BRANCH
git push -u github $BRANCH

if TAG="$(git describe --tags --exact-match 2>/dev/null)"; then
  git push -u github "$TAG"
fi
