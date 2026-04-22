#!/usr/bin/env bash
# Custom executor — prepare stage.
# Launches a fresh Incus container for the job, waits for it to be ready,
# then installs the gitlab-runner helper binary inside it.
#
# Environment variables provided by gitlab-runner:
#   CUSTOM_ENV_CI_JOB_ID        — unique job ID, used as container name
#   CUSTOM_ENV_CI_PROJECT_DIR   — path inside the container for the build
#   CUSTOM_ENV_CI_JOB_IMAGE     — OCI image name from the job's image: key
#                                  (only meaningful with a Docker executor;
#                                   here we use it to select an Incus image alias)

set -euo pipefail

# ── Config ────────────────────────────────────────────────────────────────────

CONTAINER_NAME="ci-job-${CUSTOM_ENV_CI_JOB_ID}"
INCUS_PROFILE="${INCUS_RUNNER_PROFILE:-gitlab-runner}"
# Default base image: Ubuntu 24.04 LTS from the Incus image server.
# Override per-job by setting INCUS_IMAGE in .gitlab-ci.yml variables.
INCUS_IMAGE="${CUSTOM_ENV_INCUS_IMAGE:-ubuntu:24.04}"
RUNNER_HELPER_DIR="/usr/local/lib/gitlab-runner"
BOOT_TIMEOUT=60   # seconds to wait for cloud-init / network

# ── Launch container ──────────────────────────────────────────────────────────

echo "Launching container ${CONTAINER_NAME} (image: ${INCUS_IMAGE}, profile: ${INCUS_PROFILE})"

incus launch "${INCUS_IMAGE}" "${CONTAINER_NAME}" \
  --profile default \
  --profile "${INCUS_PROFILE}" \
  --ephemeral

# ── Wait for network ──────────────────────────────────────────────────────────

echo "Waiting for container to be ready..."
deadline=$(( $(date +%s) + BOOT_TIMEOUT ))
while true; do
  state=$(incus info "${CONTAINER_NAME}" 2>/dev/null | awk '/^Status:/{print $2}')
  if [[ "$state" == "RUNNING" ]]; then
    # Check that the network is up (cloud-init may still be running)
    if incus exec "${CONTAINER_NAME}" -- test -f /run/cloud-init/result.json 2>/dev/null; then
      break
    fi
    # Fallback: just check that we can exec
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

# ── Install gitlab-runner helper ──────────────────────────────────────────────
# The helper binary is used by gitlab-runner to upload artefacts and cache.

HELPER_PATH=$(gitlab-runner --version 2>/dev/null | awk '/^Version:/{print $2}' || echo "latest")
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
" 2>/dev/null || {
  # Non-fatal: artefact upload will fail but the job can still run
  echo "WARNING: could not install gitlab-runner-helper inside container" >&2
}

# ── Create build directory ────────────────────────────────────────────────────

incus exec "${CONTAINER_NAME}" -- mkdir -p /builds /cache

echo "Container ${CONTAINER_NAME} ready"
