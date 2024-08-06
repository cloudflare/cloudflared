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

# Add code signing private key to the key chain
if [[ ! -z "$CFD_CODE_SIGN_KEY" ]]; then
  if [[ ! -z "$CFD_CODE_SIGN_PASS" ]]; then
    # write private key to disk and then import it keychain
    echo -n -e ${CFD_CODE_SIGN_KEY} | base64 -D > ${CODE_SIGN_PRIV}
    # we set || true  here and for every `security import invoke` because the  "duplicate SecKeychainItemImport" error 
    # will cause set -e to exit 1. It is okay we do this because we deliberately handle this error in the lines below.
    out=$(security import ${CODE_SIGN_PRIV} -A -P "${CFD_CODE_SIGN_PASS}" 2>&1) || true 
    exitcode=$?
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
    rm ${CODE_SIGN_PRIV}
  fi
fi

# Add code signing certificate to the key chain
if [[ ! -z "$CFD_CODE_SIGN_CERT" ]]; then
  # write certificate to disk and then import it keychain
  echo -n -e ${CFD_CODE_SIGN_CERT} | base64 -D > ${CODE_SIGN_CERT}
  out1=$(security import ${CODE_SIGN_CERT} -A 2>&1) || true
  exitcode1=$?
  if [ -n "$out1" ]; then
    if [ $exitcode1 -eq 0 ]; then
        echo "$out1"
    else
        if [ "$out1" != "${SEC_DUP_MSG}" ]; then
            echo "$out1" >&2
            exit $exitcode1
        else 
            echo "already imported code signing certificate"
        fi
    fi
  fi
  rm ${CODE_SIGN_CERT}
fi

# Add package signing private key to the key chain
if [[ ! -z "$CFD_INSTALLER_KEY" ]]; then
  if [[ ! -z "$CFD_INSTALLER_PASS" ]]; then
    # write private key to disk and then import it into the keychain
    echo -n -e ${CFD_INSTALLER_KEY} | base64 -D > ${INSTALLER_PRIV}
    out2=$(security import ${INSTALLER_PRIV} -A -P "${CFD_INSTALLER_PASS}" 2>&1) || true
    exitcode2=$?
    if [ -n "$out2" ]; then
      if [ $exitcode2 -eq 0 ]; then
          echo "$out2"
      else
          if [ "$out2" != "${SEC_DUP_MSG}" ]; then
              echo "$out2" >&2
              exit $exitcode2
          fi
      fi
    fi
    rm ${INSTALLER_PRIV}
  fi
fi

# Add package signing certificate to the key chain
if [[ ! -z "$CFD_INSTALLER_CERT" ]]; then
  # write certificate to disk and then import it keychain
  echo -n -e ${CFD_INSTALLER_CERT} | base64 -D > ${INSTALLER_CERT}
  out3=$(security import ${INSTALLER_CERT} -A 2>&1) || true
  exitcode3=$?
  if [ -n "$out3" ]; then
    if [ $exitcode3 -eq 0 ]; then
        echo "$out3"
    else
        if [ "$out3" != "${SEC_DUP_MSG}" ]; then
            echo "$out3" >&2
            exit $exitcode3
        else 
            echo "already imported installer certificate"
        fi
    fi
  fi
  rm ${INSTALLER_CERT}
fi

# get the code signing certificate name
if [[ ! -z "$CFD_CODE_SIGN_NAME" ]]; then
  CODE_SIGN_NAME="${CFD_CODE_SIGN_NAME}"
else
  if [[ -n "$(security find-certificate -c "Developer ID Application" | cut -d'"' -f 4 -s | grep "Developer ID Application:" | head -1)" ]]; then
    CODE_SIGN_NAME=$(security find-certificate -c "Developer ID Application" | cut -d'"' -f 4 -s | grep "Developer ID Application:" | head -1)
  else
    CODE_SIGN_NAME=""
  fi
fi

# get the package signing certificate name
if [[ ! -z "$CFD_INSTALLER_NAME" ]]; then
  PKG_SIGN_NAME="${CFD_INSTALLER_NAME}"
else
  if [[ -n "$(security find-certificate -c "Developer ID Installer" | cut -d'"' -f 4 -s | grep "Developer ID Installer:" | head -1)" ]]; then
    PKG_SIGN_NAME=$(security find-certificate -c "Developer ID Installer" | cut -d'"' -f 4 -s | grep "Developer ID Installer:" | head -1)
  else
    PKG_SIGN_NAME=""
  fi
fi

# cleanup the build directory because the previous execution might have failed without cleaning up.
rm -rf "${TARGET_DIRECTORY}"
export TARGET_OS="darwin"
GOCACHE="$PWD/../../../../" GOPATH="$PWD/../../../../" CGO_ENABLED=1 make cloudflared

# sign the cloudflared binary
if [[ ! -z "$CODE_SIGN_NAME" ]]; then
  codesign -s "${CODE_SIGN_NAME}" -f -v --timestamp --options runtime ${BINARY_NAME}

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
