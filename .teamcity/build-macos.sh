#!/bin/bash

set -euo pipefail

if [[ "$(uname)" != "Darwin" ]] ; then
    echo "This should be run on macOS"
    exit 1
fi

go version
export GO111MODULE=on

# build 'cloudflared-darwin-amd64.tgz'
mkdir -p artifacts
FILENAME="$(pwd)/artifacts/cloudflared-darwin-amd64.tgz"
PKGNAME="$(pwd)/artifacts/cloudflared-amd64.pkg"
TARGET_DIRECTORY=".build"
BINARY_NAME="cloudflared"
VERSION=$(git describe --tags --always --dirty="-dev")
PRODUCT="cloudflared"
CODE_SIGN_PRIV="code_sign.p12"
CODE_SIGN_CERT="code_sign.cer"
INSTALLER_PRIV="installer.p12"
INSTALLER_CERT="installer.cer"
export PATH="$PATH:/usr/local/bin"
mkdir -p ../src/github.com/cloudflare/    
cp -r . ../src/github.com/cloudflare/cloudflared
cd ../src/github.com/cloudflare/cloudflared 
GOCACHE="$PWD/../../../../" GOPATH="$PWD/../../../../" CGO_ENABLED=1 make cloudflared

# Add code signing private key to the key chain
if [[ -n "${CFD_CODE_SIGN_KEY:-}" ]]; then
  if [[ -n "${CFD_CODE_SIGN_PASS:-}" ]]; then
    # write private key to disk and then import it keychain
    echo -n -e ${CFD_CODE_SIGN_KEY} | base64 -D > ${CODE_SIGN_PRIV}
    security import ${CODE_SIGN_PRIV} -A -P "${CFD_CODE_SIGN_PASS}"
    rm ${CODE_SIGN_PRIV}
  fi
fi

# Add code signing certificate to the key chain
if [[ -n "${CFD_CODE_SIGN_CERT:-}" ]]; then
  # write certificate to disk and then import it keychain
  echo -n -e ${CFD_CODE_SIGN_CERT} | base64 -D > ${CODE_SIGN_CERT}
  security import ${CODE_SIGN_CERT}
  rm ${CODE_SIGN_CERT}
fi

# Add package signing private key to the key chain
if [[ -n "${CFD_INSTALLER_KEY:-}" ]]; then
  if [[ -n "${CFD_INSTALLER_PASS:-}" ]]; then
    # write private key to disk and then import it into the keychain
    echo -n -e ${CFD_INSTALLER_KEY} | base64 -D > ${INSTALLER_PRIV}
    security import ${INSTALLER_PRIV} -A -P "${CFD_INSTALLER_PASS}"
    rm ${INSTALLER_PRIV}
  fi
fi

# Add package signing certificate to the key chain
if [[ -n "${CFD_INSTALLER_CERT:-}" ]]; then
  # write certificate to disk and then import it keychain
  echo -n -e ${CFD_INSTALLER_CERT} | base64 -D > ${INSTALLER_CERT}
  security import ${INSTALLER_CERT}
  rm ${INSTALLER_CERT}
fi

# get the code signing certificate name
if [[ -n "${CFD_CODE_SIGN_NAME:-}" ]]; then
  CODE_SIGN_NAME="${CFD_CODE_SIGN_NAME}"
else
  if [[ -n "$(security find-identity -v | cut -d'"' -f 2 -s | grep "Developer ID Application:")" ]]; then
    CODE_SIGN_NAME=$(security find-identity -v | cut -d'"' -f 2 -s | grep "Developer ID Application:")
  else
    CODE_SIGN_NAME=""
  fi
fi

# get the package signing certificate name
if [[ -n "${CFD_INSTALLER_NAME:-}" ]]; then
  PKG_SIGN_NAME="${CFD_INSTALLER_NAME}"
else
  if [[ -n "$(security find-identity -v | cut -d'"' -f 2 -s | grep "Developer ID Installer:")" ]]; then
    PKG_SIGN_NAME=$(security find-identity -v | cut -d'"' -f 2 -s | grep "Developer ID Installer:")
  else
    PKG_SIGN_NAME=""
  fi
fi

# sign the cloudflared binary
if [[ -n "${CODE_SIGN_NAME:-}" ]]; then
  codesign -s "${CODE_SIGN_NAME}" -f -v --timestamp --options runtime ${BINARY_NAME}
fi


# creating build directory
mkdir "${TARGET_DIRECTORY}"
mkdir "${TARGET_DIRECTORY}/contents"
cp -r ".mac_resources/scripts" "${TARGET_DIRECTORY}/scripts"

# copy cloudflared into the build directory
cp ${BINARY_NAME} "${TARGET_DIRECTORY}/contents/${PRODUCT}"

# compress cloudflared into a tar and gzipped file
tar czf "$FILENAME" "${BINARY_NAME}"

# build the installer package
if [[ -n "${PKG_SIGN_NAME:-}" ]]; then
  pkgbuild --identifier com.cloudflare.${PRODUCT} \
      --version ${VERSION} \
      --scripts ${TARGET_DIRECTORY}/scripts \
      --root ${TARGET_DIRECTORY}/contents \
      --install-location /usr/local/bin \
      --sign "${PKG_SIGN_NAME}" \
      ${PKGNAME}
else
    pkgbuild --identifier com.cloudflare.${PRODUCT} \
      --version ${VERSION} \
      --scripts ${TARGET_DIRECTORY}/scripts \
      --root ${TARGET_DIRECTORY}/contents \
      --install-location /usr/local/bin \
      ${PKGNAME}
fi


# cleaning up the build directory
rm -rf $TARGET_DIRECTORY
