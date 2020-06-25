#!/bin/bash

TARGET_DIRECTORY=".build"
BINARY_NAME="cloudflared"
VERSION=$(git describe --tags --always --dirty="-dev")
PRODUCT="cloudflared"


echo "building cloudflared"
make cloudflared

echo "creating build directory"
mkdir ${TARGET_DIRECTORY}
mkdir ${TARGET_DIRECTORY}/contents
cp -r .mac_resources/scripts ${TARGET_DIRECTORY}/scripts

echo "move cloudflared into the build directory"
mv $BINARY_NAME {$TARGET_DIRECTORY}/contents/${PRODUCT}

echo "build the installer package"
pkgbuild --identifier com.cloudflare.${PRODUCT} \
    --version ${VERSION} \
    --scripts ${TARGET_DIRECTORY}/scripts \
    --root ${TARGET_DIRECTORY}/contents \
    --install-location /usr/local/bin \
    ${PRODUCT}.pkg
    # TODO: our iOS/Mac account doesn't have this installer certificate type. 
    # need to find how we can get it --sign "Developer ID Installer: Cloudflare" \
    
echo "cleaning up the build directory"
rm -rf $TARGET_DIRECTORY
