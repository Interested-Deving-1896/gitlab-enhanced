#!/usr/bin/env bash
# Custom executor — prepare stage.
# Launches a fresh Incus container for the job and waits for it to be ready.
#
# Environment variables provided by gitlab-runner:
#   CUSTOM_ENV_CI_JOB_ID    — unique job ID, used as the container name
#   CUSTOM_ENV_INCUS_IMAGE  — Incus image alias to launch (set in .gitlab-ci.yml)
#
# Default image: gitlab-enhanced/ci-base:latest
# Built by:      runtime/incus/images/build-ci-base.sh
# Contains:      Go, gitlab-runner-helper, gotestsum, shellcheck, yamllint, git
#
# Fallback image: ubuntu:24.04 (used if the pre-baked image is not present)
# When using the fallback, gitlab-runner-helper is installed at runtime.

set -euo pipefail

# ── Config ────────────────────────────────────────────────────────────────────

CONTAINER_NAME="ci-job-${CUSTOM_ENV_CI_JOB_ID}"
INCUS_PROFILE="${INCUS_RUNNER_PROFILE:-gitlab-runner}"
PREBAKED_IMAGE="gitlab-enhanced/ci-base:latest"
FALLBACK_IMAGE="ubuntu:24.04"
RUNNER_HELPER_DIR="/usr/local/lib/gitlab-runner"
BOOT_TIMEOUT=60

# Prefer the pre-baked image; allow per-job override via INCUS_IMAGE variable.
if [[ -n "${CUSTOM_ENV_INCUS_IMAGE:-}" ]]; then
  INCUS_IMAGE="${CUSTOM_ENV_INCUS_IMAGE}"
elif incus image info "${PREBAKED_IMAGE}" &>/dev/null; then
  INCUS_IMAGE="${PREBAKED_IMAGE}"
else
  echo "WARNING: pre-baked image '${PREBAKED_IMAGE}' not found — falling back to ${FALLBACK_IMAGE}" >&2
  echo "         Run 'bash runtime/incus/images/build-ci-base.sh' to build it." >&2
  INCUS_IMAGE="${FALLBACK_IMAGE}"
fi

# ── Launch container ──────────────────────────────────────────────────────────

echo "Launching container ${CONTAINER_NAME} (image: ${INCUS_IMAGE}, profile: ${INCUS_PROFILE})"

incus launch "${INCUS_IMAGE}" "${CONTAINER_NAME}" \
  --profile default \
  --profile "${INCUS_PROFILE}" \
  --ephemeral

# ── Wait for container to be ready ───────────────────────────────────────────

echo "Waiting for container to be ready..."
deadline=$(( $(date +%s) + BOOT_TIMEOUT ))
while true; do
  state=$(incus info "${CONTAINER_NAME}" 2>/dev/null | awk '/^Status:/{print $2}')
  if [[ "$state" == "RUNNING" ]]; then
    if incus exec "${CONTAINER_NAME}" -- true 2>/dev/null; then
      break
    fi
  fi
  if (( $(date +%s) > deadline )); then
    echo "ERROR: container ${CONTAINER_NAME} did not become ready within ${BOOT_TIMEOUT}s" >&2
    incus delete --force "${CONTAINER_NAME}" 2>/dev/null || true
    exit 1
  fi
  sleep 2
done

# ── Fallback: install gitlab-runner-helper if not in the image ───────────────
# Skipped when using the pre-baked image (helper is already present).

if ! incus exec "${CONTAINER_NAME}" -- test -x "${RUNNER_HELPER_DIR}/gitlab-runner-helper" 2>/dev/null; then
  echo "gitlab-runner-helper not found in image — installing at runtime"
  ARCH=$(incus exec "${CONTAINER_NAME}" -- uname -m 2>/dev/null)
  case "$ARCH" in
    x86_64)  HELPER_ARCH="amd64" ;;
    aarch64) HELPER_ARCH="arm64" ;;
    *)       HELPER_ARCH="amd64" ;;
  esac
  incus exec "${CONTAINER_NAME}" -- mkdir -p "${RUNNER_HELPER_DIR}"
  incus exec "${CONTAINER_NAME}" -- bash -c "
    curl -fsSL \
      'https://gitlab-runner-downloads.s3.amazonaws.com/latest/binaries/gitlab-runner-helper/gitlab-runner-helper.linux-${HELPER_ARCH}' \
      -o '${RUNNER_HELPER_DIR}/gitlab-runner-helper' \
    && chmod +x '${RUNNER_HELPER_DIR}/gitlab-runner-helper'
  " || echo "WARNING: could not install gitlab-runner-helper" >&2
fi

# ── Create standard directories ───────────────────────────────────────────────

incus exec "${CONTAINER_NAME}" -- mkdir -p /builds /cache

echo "Container ${CONTAINER_NAME} ready (image: ${INCUS_IMAGE})"
