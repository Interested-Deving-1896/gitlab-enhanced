#!/usr/bin/env bash
# Create a versioned release: tag, build binaries, package deb/rpm.
#
# Usage:
#   ./scripts/release.sh 1.2.3
#
# Outputs:
#   dist/gitlab-enhanced-1.2.3-linux-amd64
#   dist/gitlab-enhanced_1.2.3_amd64.deb
#   dist/gitlab-enhanced-1.2.3-1.x86_64.rpm
set -euo pipefail

VERSION="${1:-}"
if [[ -z "${VERSION}" ]]; then
  echo "Usage: $0 <version>" >&2
  exit 1
fi

# Validate semver (loose: digits and dots)
if ! [[ "${VERSION}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Version must be semver (e.g. 1.2.3), got: ${VERSION}" >&2
  exit 1
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="${REPO_ROOT}/dist"
MODULE="gitlab.com/openos-project/git-management_deving/gitlab-enhanced"
COMMIT="$(git -C "${REPO_ROOT}" rev-parse --short HEAD)"
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-s -w \
  -X ${MODULE}/version.Version=${VERSION} \
  -X ${MODULE}/version.Commit=${COMMIT} \
  -X ${MODULE}/version.Date=${DATE}"

echo "==> Building gitlab-enhanced ${VERSION} (${COMMIT})"
mkdir -p "${DIST}"

# Build linux/amd64
GOOS=linux GOARCH=amd64 go build \
  -ldflags "${LDFLAGS}" \
  -o "${DIST}/gitlab-enhanced-${VERSION}-linux-amd64" \
  "${MODULE}/cmd/gitlab-enhanced"

echo "    binary: dist/gitlab-enhanced-${VERSION}-linux-amd64"

# Package with nfpm if available
if command -v nfpm &>/dev/null; then
  echo "==> Packaging with nfpm"
  NFPM_VERSION="${VERSION}" nfpm package \
    --config "${REPO_ROOT}/packaging/config/nfpm.yaml" \
    --packager deb \
    --target "${DIST}/"
  NFPM_VERSION="${VERSION}" nfpm package \
    --config "${REPO_ROOT}/packaging/config/nfpm.yaml" \
    --packager rpm \
    --target "${DIST}/"
  echo "    packages written to dist/"
else
  echo "    nfpm not found — skipping deb/rpm packaging"
  echo "    Install: https://nfpm.goreleaser.com/install/"
fi

# Tag the release
if git -C "${REPO_ROOT}" tag "v${VERSION}" 2>/dev/null; then
  echo "==> Tagged v${VERSION}"
  echo "    Push with: git push origin v${VERSION}"
else
  echo "    Tag v${VERSION} already exists — skipping"
fi

echo "==> Done: dist/"
ls -lh "${DIST}/"
