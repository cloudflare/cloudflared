#!/bin/bash

set -exo pipefail

if [[ "$(uname)" != "Darwin" ]] ; then
    echo "This should be run on macOS"
    exit 1
fi

if [[ "amd64" != "${TARGET_ARCH}" && "arm64" != "${TARGET_ARCH}" ]]
then
  echo "TARGET_ARCH must be amd64 or arm64"
  exit 1
fi

go version
export GO111MODULE=on

# build 'cloudflared-darwin-amd64.tgz'
mkdir -p artifacts
TARGET_DIRECTORY=".build"
BINARY_NAME="cloudflared"
VERSION=$(git describe --tags --always --dirty="-dev")
PRODUCT="cloudflared"
APPLE_CA_CERT="apple_dev_ca.cert"
CODE_SIGN_PRIV="code_sign.p12"
CODE_SIGN_CERT="code_sign.cer"
INSTALLER_PRIV="installer.p12"
INSTALLER_CERT="installer.cer"
BUNDLE_ID="com.cloudflare.cloudflared"
SEC_DUP_MSG="security: SecKeychainItemImport: The specified item already exists in the keychain."
export PATH="$PATH:/usr/local/bin"
FILENAME="$(pwd)/artifacts/cloudflared-darwin-$TARGET_ARCH.tgz"
PKGNAME="$(pwd)/artifacts/cloudflared-$TARGET_ARCH.pkg"
mkdir -p ../src/github.com/cloudflare/    
cp -r . ../src/github.com/cloudflare/cloudflared
cd ../src/github.com/cloudflare/cloudflared 

# Imports certificates to the Apple KeyChain
import_certificate() {
    local CERTIFICATE_NAME=$1
    local CERTIFICATE_ENV_VAR=$2
    local CERTIFICATE_FILE_NAME=$3

    echo "Importing $CERTIFICATE_NAME"

    if [[ ! -z "$CERTIFICATE_ENV_VAR" ]]; then
      # write certificate to disk and then import it keychain
      echo -n -e ${CERTIFICATE_ENV_VAR} | base64 -D > ${CERTIFICATE_FILE_NAME}
      # we set || true  here and for every `security import invoke` because the  "duplicate SecKeychainItemImport" error
      # will cause set -e to exit 1. It is okay we do this because we deliberately handle this error in the lines below.
      local out=$(security import ${CERTIFICATE_FILE_NAME} -T /usr/bin/pkgbuild -A 2>&1) || true
      local exitcode=$?
      # delete the certificate from disk
      rm -rf ${CERTIFICATE_FILE_NAME}
      if [ -n "$out" ]; then
        if [ $exitcode -eq 0 ]; then
            echo "$out"
        else
            if [ "$out" != "${SEC_DUP_MSG}" ]; then
                echo "$out" >&2
                exit $exitcode
            else
                echo "already imported code signing certificate"
            fi
        fi
      fi
    fi
}

create_cloudflared_build_keychain() {
  # Reusing the private key password as the keychain key
  local PRIVATE_KEY_PASS=$1

  # Create keychain only if it doesn't already exist
  if [ ! -f "$HOME/Library/Keychains/cloudflared_build_keychain.keychain-db" ]; then
    security create-keychain -p "$PRIVATE_KEY_PASS" cloudflared_build_keychain
  else
    echo "Keychain already exists: cloudflared_build_keychain"
  fi

  # Append temp keychain to the user domain
  security list-keychains -d user -s cloudflared_build_keychain $(security list-keychains -d user | sed s/\"//g)

  # Remove relock timeout
  security set-keychain-settings cloudflared_build_keychain

  # Unlock keychain so it doesn't require password
  security unlock-keychain -p "$PRIVATE_KEY_PASS" cloudflared_build_keychain

}

# Imports private keys to the Apple KeyChain
import_private_keys() {
    local PRIVATE_KEY_NAME=$1
    local PRIVATE_KEY_ENV_VAR=$2
    local PRIVATE_KEY_FILE_NAME=$3
    local PRIVATE_KEY_PASS=$4

    echo "Importing $PRIVATE_KEY_NAME"

    if [[ ! -z "$PRIVATE_KEY_ENV_VAR" ]]; then
      if [[ ! -z "$PRIVATE_KEY_PASS" ]]; then
        # write private key to disk and then import it keychain
        echo -n -e ${PRIVATE_KEY_ENV_VAR} | base64 -D > ${PRIVATE_KEY_FILE_NAME}
        # we set || true  here and for every `security import invoke` because the  "duplicate SecKeychainItemImport" error
        # will cause set -e to exit 1. It is okay we do this because we deliberately handle this error in the lines below.
        local out=$(security import ${PRIVATE_KEY_FILE_NAME} -k cloudflared_build_keychain -P "$PRIVATE_KEY_PASS" -T /usr/bin/pkgbuild -A -P "${PRIVATE_KEY_PASS}" 2>&1) || true
        local exitcode=$?
        rm -rf ${PRIVATE_KEY_FILE_NAME}
        if [ -n "$out" ]; then
          if [ $exitcode -eq 0 ]; then
              echo "$out"
          else
              if [ "$out" != "${SEC_DUP_MSG}" ]; then
                  echo "$out" >&2
                  exit $exitcode
              fi
          fi
        fi
      fi
    fi
}

# Create temp keychain only for this build
create_cloudflared_build_keychain "${CFD_CODE_SIGN_PASS}"

# Add Apple Root Developer certificate to the key chain
import_certificate "Apple Developer CA" "${APPLE_DEV_CA_CERT}" "${APPLE_CA_CERT}"

# Add code signing private key to the key chain
import_private_keys "Developer ID Application" "${CFD_CODE_SIGN_KEY}" "${CODE_SIGN_PRIV}" "${CFD_CODE_SIGN_PASS}"

# Add code signing certificate to the key chain
import_certificate "Developer ID Application" "${CFD_CODE_SIGN_CERT}" "${CODE_SIGN_CERT}"

# Add package signing private key to the key chain
import_private_keys "Developer ID Installer" "${CFD_INSTALLER_KEY}" "${INSTALLER_PRIV}" "${CFD_INSTALLER_PASS}"

# Add package signing certificate to the key chain
import_certificate "Developer ID Installer" "${CFD_INSTALLER_CERT}" "${INSTALLER_CERT}"

# get the code signing certificate name
if [[ ! -z "$CFD_CODE_SIGN_NAME" ]]; then
  CODE_SIGN_NAME="${CFD_CODE_SIGN_NAME}"
else
  if [[ -n "$(security find-certificate -c "Developer ID Application" cloudflared_build_keychain | cut -d'"' -f 4 -s | grep "Developer ID Application:" | head -1)" ]]; then
    CODE_SIGN_NAME=$(security find-certificate -c "Developer ID Application" cloudflared_build_keychain | cut -d'"' -f 4 -s | grep "Developer ID Application:" | head -1)
  else
    CODE_SIGN_NAME=""
  fi
fi

# get the package signing certificate name
if [[ ! -z "$CFD_INSTALLER_NAME" ]]; then
  PKG_SIGN_NAME="${CFD_INSTALLER_NAME}"
else
  if [[ -n "$(security find-certificate -c "Developer ID Installer" cloudflared_build_keychain | cut -d'"' -f 4 -s | grep "Developer ID Installer:" | head -1)" ]]; then
    PKG_SIGN_NAME=$(security find-certificate -c "Developer ID Installer" cloudflared_build_keychain | cut -d'"' -f 4 -s | grep "Developer ID Installer:" | head -1)
  else
    PKG_SIGN_NAME=""
  fi
fi

# cleanup the build directory because the previous execution might have failed without cleaning up.
rm -rf "${TARGET_DIRECTORY}"
export TARGET_OS="darwin"
GOCACHE="$PWD/../../../../" GOPATH="$PWD/../../../../" CGO_ENABLED=1 make cloudflared


# This allows apple tools to use the certificates in the keychain without requiring password input.
# This command always needs to run after the certificates have been loaded into the keychain
if [[ ! -z "$CFD_CODE_SIGN_PASS" ]]; then
  security set-key-partition-list -S apple-tool:,apple: -s -k "${CFD_CODE_SIGN_PASS}" cloudflared_build_keychain
fi

# sign the cloudflared binary
if [[ ! -z "$CODE_SIGN_NAME" ]]; then
  codesign --keychain $HOME/Library/Keychains/cloudflared_build_keychain.keychain-db -s "${CODE_SIGN_NAME}" -fv --options runtime --timestamp ${BINARY_NAME}

 # notarize the binary
 # TODO: TUN-5789
fi

ARCH_TARGET_DIRECTORY="${TARGET_DIRECTORY}/${TARGET_ARCH}-build"
# creating build directory
rm -rf $ARCH_TARGET_DIRECTORY
mkdir -p "${ARCH_TARGET_DIRECTORY}"
mkdir -p "${ARCH_TARGET_DIRECTORY}/contents"
cp -r ".mac_resources/scripts" "${ARCH_TARGET_DIRECTORY}/scripts"

# copy cloudflared into the build directory
cp ${BINARY_NAME} "${ARCH_TARGET_DIRECTORY}/contents/${PRODUCT}"

# compress cloudflared into a tar and gzipped file
tar czf "$FILENAME" "${BINARY_NAME}"

# build the installer package
if [[ ! -z "$PKG_SIGN_NAME" ]]; then

  pkgbuild --identifier com.cloudflare.${PRODUCT} \
      --version ${VERSION} \
      --scripts ${ARCH_TARGET_DIRECTORY}/scripts \
      --root ${ARCH_TARGET_DIRECTORY}/contents \
      --install-location /usr/local/bin \
      --keychain cloudflared_build_keychain \
      --sign "${PKG_SIGN_NAME}" \
      ${PKGNAME}

      # notarize the package
      # TODO: TUN-5789
else
    pkgbuild --identifier com.cloudflare.${PRODUCT} \
      --version ${VERSION} \
      --scripts ${ARCH_TARGET_DIRECTORY}/scripts \
      --root ${ARCH_TARGET_DIRECTORY}/contents \
      --install-location /usr/local/bin \
      ${PKGNAME}
fi

# cleanup build directory because this script is not ran within containers,
# which might lead to future issues in subsequent runs.
rm -rf "${TARGET_DIRECTORY}"

# cleanup the keychain
security default-keychain -d user -s login.keychain-db
security list-keychains -d user -s login.keychain-db
security delete-keychain cloudflared_build_keychain
