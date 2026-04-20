#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
OUTPUT_DIR="${SCRIPT_DIR}/output"
PACKAGE_TEMPLATE_DIR="${SCRIPT_DIR}/package"
BUILD_BINARY="${REPO_ROOT}/cloudflared"
VERSION="${1:-$(git -C "${REPO_ROOT}" describe --tags --always --match "[0-9][0-9][0-9][0-9].*.*")}" 

ARTIFACT_DIR="${OUTPUT_DIR}/${VERSION}"
STAGING_DIR="${ARTIFACT_DIR}/cloudflared-koolshare-arm32"
TARBALL_PATH="${ARTIFACT_DIR}/cloudflared-koolshare-arm32-${VERSION}.tar.gz"

rm -rf "${STAGING_DIR}"
mkdir -p "${STAGING_DIR}"

printf '[1/5] Building cloudflared for linux/armv7...\n'
(
  cd "${REPO_ROOT}"
  TARGET_OS=linux TARGET_ARCH=arm TARGET_ARM=7 make cloudflared
)

printf '[2/5] Preparing package layout...\n'
cp -R "${PACKAGE_TEMPLATE_DIR}/." "${STAGING_DIR}/"
install -m 0755 "${BUILD_BINARY}" "${STAGING_DIR}/bin/cloudflared"

printf '[3/5] Writing version metadata...\n'
cat > "${STAGING_DIR}/VERSION" <<META
${VERSION}
META

printf '[4/5] Creating tarball...\n'
mkdir -p "${ARTIFACT_DIR}"
tar -C "${ARTIFACT_DIR}" -czf "${TARBALL_PATH}" "$(basename "${STAGING_DIR}")"

printf '[5/5] Calculating SHA256...\n'
(
  cd "${ARTIFACT_DIR}"
  sha256sum "$(basename "${TARBALL_PATH}")" > "$(basename "${TARBALL_PATH}").sha256"
)

printf 'Done.\nPackage: %s\nChecksum: %s\n' \
  "${TARBALL_PATH}" \
  "${TARBALL_PATH}.sha256"
