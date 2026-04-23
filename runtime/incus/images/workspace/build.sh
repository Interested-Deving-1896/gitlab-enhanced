#!/usr/bin/env bash
# Build the gitlab-enhanced workspace image and publish it to the local Incus image store.
#
# The image is built using BuildKit (via the buildkit Incus instance) and then
# imported into Incus as "gitlab-enhanced/workspace-full:latest".
#
# Usage:
#   bash runtime/incus/images/workspace/build.sh
#
# Override versions:
#   GO_VERSION=1.24.2 OPENVSCODE_VERSION=1.89.1 bash runtime/incus/images/workspace/build.sh
#
# Requirements:
#   - Incus installed and initialised
#   - BuildKit instance running (incus start buildkit) OR buildctl in PATH
#   - skopeo in PATH (for OCI → Incus image import)

set -euo pipefail

# ── Versions ──────────────────────────────────────────────────────────────────
GO_VERSION="${GO_VERSION:-1.24.2}"
NODE_VERSION="${NODE_VERSION:-20}"
OPENVSCODE_VERSION="${OPENVSCODE_VERSION:-1.89.1}"

# ── Config ────────────────────────────────────────────────────────────────────
IMAGE_ALIAS="gitlab-enhanced/workspace-full:latest"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../../.." && pwd)"
BUILD_DIR="${SCRIPT_DIR}"
OCI_ARCHIVE="/tmp/workspace-full-$$.tar"
BUILDKIT_ADDR="${BUILDKIT_ADDR:-unix:///run/buildkit/buildkitd.sock}"

# ── Colours ───────────────────────────────────────────────────────────────────
BLUE='\033[1;34m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RESET='\033[0m'
info()  { echo -e "${BLUE}▶${RESET}  $*"; }
ok()    { echo -e "  ${GREEN}✓${RESET}  $*"; }
warn()  { echo -e "  ${YELLOW}⚠${RESET}  $*"; }

# ── Cleanup ───────────────────────────────────────────────────────────────────
cleanup() { rm -f "${OCI_ARCHIVE}" "${OCI_ARCHIVE}.gz" 2>/dev/null || true; }
trap cleanup EXIT

# ── Detect architecture ───────────────────────────────────────────────────────
HOST_ARCH="$(uname -m)"
case "$HOST_ARCH" in
  x86_64)  TARGETARCH="amd64" ;;
  aarch64) TARGETARCH="arm64" ;;
  *)       echo "Unsupported architecture: $HOST_ARCH" >&2; exit 1 ;;
esac

# ── Determine BuildKit address ────────────────────────────────────────────────
# Prefer the buildkit Incus instance socket if the instance is running.
if incus info buildkit &>/dev/null 2>&1; then
  STATE=$(incus info buildkit 2>/dev/null | awk '/^Status:/{print $2}')
  if [[ "$STATE" == "RUNNING" ]]; then
    info "Using BuildKit from Incus instance 'buildkit'"
    # Expose socket via incus exec proxy
    USE_INCUS_BUILDKIT=1
  fi
fi

# ── Remove existing image ─────────────────────────────────────────────────────
if incus image info "${IMAGE_ALIAS}" &>/dev/null; then
  warn "Removing existing image ${IMAGE_ALIAS}"
  incus image delete "${IMAGE_ALIAS}"
fi

# ── Build OCI image ───────────────────────────────────────────────────────────
info "Building workspace image (Go ${GO_VERSION}, OpenVSCode ${OPENVSCODE_VERSION})"

BUILD_ARGS=(
  "--build-arg" "GO_VERSION=${GO_VERSION}"
  "--build-arg" "NODE_VERSION=${NODE_VERSION}"
  "--build-arg" "OPENVSCODE_VERSION=${OPENVSCODE_VERSION}"
  "--build-arg" "TARGETARCH=${TARGETARCH}"
)

if [[ "${USE_INCUS_BUILDKIT:-0}" == "1" ]]; then
  # Build via the Incus buildkit instance
  incus exec buildkit -- buildctl build \
    --frontend dockerfile.v0 \
    --local context="${BUILD_DIR}" \
    --local dockerfile="${BUILD_DIR}" \
    "${BUILD_ARGS[@]/#/--opt build-arg:}" \
    --output "type=oci,dest=/tmp/workspace-full.tar"
  incus file pull "buildkit/tmp/workspace-full.tar" "${OCI_ARCHIVE}"
  incus exec buildkit -- rm -f /tmp/workspace-full.tar
elif command -v buildctl &>/dev/null; then
  # Build via local buildctl
  buildctl build \
    --addr "${BUILDKIT_ADDR}" \
    --frontend dockerfile.v0 \
    --local "context=${BUILD_DIR}" \
    --local "dockerfile=${BUILD_DIR}" \
    "${BUILD_ARGS[@]/#/--opt build-arg:}" \
    --output "type=oci,dest=${OCI_ARCHIVE}"
elif command -v docker &>/dev/null; then
  # Fallback: build with Docker, export as OCI archive
  warn "buildctl not found — falling back to Docker"
  docker build \
    "${BUILD_ARGS[@]}" \
    --tag "gitlab-enhanced/workspace-full:latest" \
    "${BUILD_DIR}"
  docker save "gitlab-enhanced/workspace-full:latest" -o "${OCI_ARCHIVE}"
else
  echo "ERROR: no build tool found. Install buildctl or docker, or start the buildkit Incus instance." >&2
  exit 1
fi
ok "OCI image built"

# ── Import into Incus ─────────────────────────────────────────────────────────
info "Importing image into Incus as ${IMAGE_ALIAS}"

if command -v skopeo &>/dev/null; then
  # Convert OCI archive → Incus-compatible format via skopeo
  skopeo copy \
    "oci-archive:${OCI_ARCHIVE}" \
    "lxd:${IMAGE_ALIAS}" \
    --dest-tls-verify=false
else
  # Direct import — works for Docker-format archives
  incus image import "${OCI_ARCHIVE}" --alias "${IMAGE_ALIAS}"
fi

ok "Image imported: ${IMAGE_ALIAS}"

echo
echo -e "${GREEN}Workspace image build complete.${RESET}"
echo
echo "Verify:  incus image info '${IMAGE_ALIAS}'"
echo "Test:    incus launch '${IMAGE_ALIAS}' test-ws && incus exec test-ws -- go version"
echo "Use:     set environment.workspace_image: gitlab-enhanced/workspace-full:latest in config/local.yaml"
