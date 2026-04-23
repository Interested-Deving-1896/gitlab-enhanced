#!/usr/bin/env bash
# Build the gitlab-enhanced CI base image for the Incus custom executor.
#
# The resulting image is stored in the local Incus image store under the alias
# "gitlab-enhanced/ci-base:latest" and can be exported for use on other hosts.
#
# What gets baked in:
#   - Ubuntu 24.04 LTS base
#   - Go (version pinned to GO_VERSION)
#   - gitlab-runner-helper (version pinned to RUNNER_VERSION)
#   - gotestsum (version pinned to GOTESTSUM_VERSION)
#   - shellcheck, yamllint, git, curl, ca-certificates, jq
#
# Usage:
#   bash runtime/incus/images/build-ci-base.sh
#
# Override versions:
#   GO_VERSION=1.24.2 RUNNER_VERSION=17.11.0 bash runtime/incus/images/build-ci-base.sh
#
# Export to another host:
#   incus image export gitlab-enhanced/ci-base:latest ci-base.tar.gz
#   # on the target host:
#   incus image import ci-base.tar.gz --alias gitlab-enhanced/ci-base:latest

set -euo pipefail

# ── Versions ──────────────────────────────────────────────────────────────────
GO_VERSION="${GO_VERSION:-1.24.2}"
RUNNER_VERSION="${RUNNER_VERSION:-17.11.0}"
GOTESTSUM_VERSION="${GOTESTSUM_VERSION:-1.12.0}"

# ── Config ────────────────────────────────────────────────────────────────────
BUILD_CONTAINER="ci-base-build-$$"
IMAGE_ALIAS="gitlab-enhanced/ci-base:latest"
BASE_IMAGE="ubuntu:24.04"
RUNNER_HELPER_DIR="/usr/local/lib/gitlab-runner"
BOOT_TIMEOUT=120

# ── Colours ───────────────────────────────────────────────────────────────────
BLUE='\033[1;34m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RESET='\033[0m'
info()  { echo -e "${BLUE}▶${RESET}  $*"; }
ok()    { echo -e "  ${GREEN}✓${RESET}  $*"; }
warn()  { echo -e "  ${YELLOW}⚠${RESET}  $*"; }

# ── Detect architecture ───────────────────────────────────────────────────────
HOST_ARCH="$(uname -m)"
case "$HOST_ARCH" in
  x86_64)  GOARCH="amd64"; HELPER_ARCH="amd64" ;;
  aarch64) GOARCH="arm64"; HELPER_ARCH="arm64" ;;
  *)       echo "Unsupported architecture: $HOST_ARCH" >&2; exit 1 ;;
esac

# ── Cleanup on exit ───────────────────────────────────────────────────────────
cleanup() {
  if incus info "${BUILD_CONTAINER}" &>/dev/null; then
    warn "Cleaning up build container ${BUILD_CONTAINER}"
    incus delete --force "${BUILD_CONTAINER}" 2>/dev/null || true
  fi
}
trap cleanup EXIT

# ── Remove existing image alias ───────────────────────────────────────────────
if incus image info "${IMAGE_ALIAS}" &>/dev/null; then
  warn "Removing existing image ${IMAGE_ALIAS}"
  incus image delete "${IMAGE_ALIAS}"
fi

# ── Launch build container ────────────────────────────────────────────────────
info "Launching build container from ${BASE_IMAGE}"
incus launch "${BASE_IMAGE}" "${BUILD_CONTAINER}" \
  --profile default \
  --ephemeral=false   # must be non-ephemeral so we can publish it

# Wait for cloud-init
info "Waiting for container to be ready (up to ${BOOT_TIMEOUT}s)"
deadline=$(( $(date +%s) + BOOT_TIMEOUT ))
while true; do
  if incus exec "${BUILD_CONTAINER}" -- test -f /run/cloud-init/result.json 2>/dev/null; then
    break
  fi
  if (( $(date +%s) > deadline )); then
    echo "ERROR: container did not become ready within ${BOOT_TIMEOUT}s" >&2
    exit 1
  fi
  sleep 2
done
ok "Container ready"

# ── System packages ───────────────────────────────────────────────────────────
info "Installing system packages"
incus exec "${BUILD_CONTAINER}" -- bash -c "
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq \
    git curl ca-certificates jq \
    shellcheck \
    python3 python3-pip pipx \
    build-essential
  pipx install yamllint
  ln -sf /root/.local/bin/yamllint /usr/local/bin/yamllint
  apt-get clean
  rm -rf /var/lib/apt/lists/*
"
ok "System packages installed"

# ── Go ────────────────────────────────────────────────────────────────────────
info "Installing Go ${GO_VERSION}"
incus exec "${BUILD_CONTAINER}" -- bash -c "
  curl -fsSL 'https://go.dev/dl/go${GO_VERSION}.linux-${GOARCH}.tar.gz' \
    | tar -C /usr/local -xz
  echo 'export PATH=/usr/local/go/bin:/go/bin:\$PATH' >> /etc/environment
  echo 'export GOPATH=/go' >> /etc/environment
  mkdir -p /go
"
ok "Go ${GO_VERSION} installed"

# ── gotestsum ─────────────────────────────────────────────────────────────────
info "Installing gotestsum ${GOTESTSUM_VERSION}"
incus exec "${BUILD_CONTAINER}" -- bash -c "
  export PATH=/usr/local/go/bin:/go/bin:\$PATH
  export GOPATH=/go
  go install gotest.tools/gotestsum@v${GOTESTSUM_VERSION}
  cp /go/bin/gotestsum /usr/local/bin/gotestsum
"
ok "gotestsum installed"

# ── gitlab-runner-helper ──────────────────────────────────────────────────────
info "Installing gitlab-runner-helper ${RUNNER_VERSION}"
incus exec "${BUILD_CONTAINER}" -- bash -c "
  mkdir -p '${RUNNER_HELPER_DIR}'
  curl -fsSL \
    'https://gitlab-runner-downloads.s3.amazonaws.com/v${RUNNER_VERSION}/binaries/gitlab-runner-helper/gitlab-runner-helper.linux-${HELPER_ARCH}' \
    -o '${RUNNER_HELPER_DIR}/gitlab-runner-helper'
  chmod +x '${RUNNER_HELPER_DIR}/gitlab-runner-helper'
  ln -sf '${RUNNER_HELPER_DIR}/gitlab-runner-helper' /usr/local/bin/gitlab-runner-helper
"
ok "gitlab-runner-helper ${RUNNER_VERSION} installed"

# ── Bake metadata ─────────────────────────────────────────────────────────────
info "Writing image metadata"
incus exec "${BUILD_CONTAINER}" -- bash -c "
  mkdir -p /etc/gitlab-enhanced
  cat > /etc/gitlab-enhanced/ci-base.json <<EOF
{
  \"image\": \"gitlab-enhanced/ci-base\",
  \"built_at\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",
  \"go_version\": \"${GO_VERSION}\",
  \"runner_helper_version\": \"${RUNNER_VERSION}\",
  \"gotestsum_version\": \"${GOTESTSUM_VERSION}\"
}
EOF
"

# ── Pre-create standard directories ───────────────────────────────────────────
incus exec "${BUILD_CONTAINER}" -- bash -c "
  mkdir -p /builds /cache /go
"

# ── Stop container before publishing ─────────────────────────────────────────
info "Stopping build container"
incus stop "${BUILD_CONTAINER}"

# ── Publish as image ──────────────────────────────────────────────────────────
info "Publishing image as ${IMAGE_ALIAS}"
incus publish "${BUILD_CONTAINER}" \
  --alias "${IMAGE_ALIAS}" \
  description="gitlab-enhanced CI base (Go ${GO_VERSION}, runner-helper ${RUNNER_VERSION})"

ok "Image published: ${IMAGE_ALIAS}"

# Trap will delete the (now stopped) build container
echo
echo -e "${GREEN}Build complete.${RESET}"
echo
echo "Verify:  incus image info '${IMAGE_ALIAS}'"
echo "Test:    incus launch '${IMAGE_ALIAS}' test-ci && incus exec test-ci -- go version"
echo "Export:  incus image export '${IMAGE_ALIAS}' ci-base.tar.gz"
