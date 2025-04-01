#!/bin/bash
VERSION=$(git describe --tags --always --match "[0-9][0-9][0-9][0-9].*.*")
echo $VERSION

# This controls the directory the built artifacts go into
export ARTIFACT_DIR=artifacts/
mkdir -p $ARTIFACT_DIR

arch=("amd64")
export TARGET_ARCH=$arch
export TARGET_OS=linux
export FIPS=true
# For BoringCrypto to link, we need CGO enabled. Otherwise compilation fails.
export CGO_ENABLED=1

make cloudflared-deb
mv cloudflared-fips\_$VERSION\_$arch.deb $ARTIFACT_DIR/cloudflared-fips-linux-$arch.deb

# rpm packages invert the - and _ and use x86_64 instead of amd64.
RPMVERSION=$(echo $VERSION | sed -r 's/-/_/g')
RPMARCH="x86_64"
make cloudflared-rpm
mv cloudflared-fips-$RPMVERSION-1.$RPMARCH.rpm $ARTIFACT_DIR/cloudflared-fips-linux-$RPMARCH.rpm

# finally move the linux binary as well.
mv ./cloudflared $ARTIFACT_DIR/cloudflared-fips-linux-$arch
