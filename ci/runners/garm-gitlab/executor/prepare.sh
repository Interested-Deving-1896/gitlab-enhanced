#!/usr/bin/env bash
# prepare.sh — custom executor prepare stage.
#
# Called once per job before any scripts run. Launches an ephemeral Incus
# container for the job and optionally installs live-build tooling when
# privileged mode is requested.
#
# Exit codes (per GitLab custom executor spec):
#   0  — success
#   1  — system failure (job will be retried)
#   2  — build failure (job fails, no retry)

currentDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${currentDir}/base.sh"
set -eo pipefail

echo "Preparing container: ${CONTAINER_ID} (image: ${CONTAINER_IMAGE})"

# Build the incus launch argument list.
LAUNCH_ARGS="--ephemeral"

if [ "${CONTAINER_PRIVILEGED}" = "true" ]; then
    LAUNCH_ARGS="${LAUNCH_ARGS} --config security.privileged=true --config security.nesting=true"
    echo "Privileged mode enabled (security.privileged + security.nesting)"
fi

# Launch the ephemeral container. The --ephemeral flag means Incus deletes it
# automatically when it stops, so cleanup.sh only needs to force-stop it.
incus launch "${CONTAINER_IMAGE}" "${CONTAINER_ID}" ${LAUNCH_ARGS}

# Wait for the container to have network connectivity (up to 60 s).
echo "Waiting for network in ${CONTAINER_ID}..."
for i in $(seq 1 30); do
    if incus exec "${CONTAINER_ID}" -- sh -c "ping -c1 -W2 8.8.8.8 >/dev/null 2>&1"; then
        echo "Network ready after $((i * 2))s"
        break
    fi
    if [ "${i}" -eq 30 ]; then
        echo "ERROR: network did not become available within 60s" >&2
        exit 1
    fi
    sleep 2
done

# Install live-build toolchain when running in privileged mode.
# This is idempotent — safe to run even if packages are already present.
if [ "${CONTAINER_PRIVILEGED}" = "true" ]; then
    echo "Installing live-build dependencies..."
    incus exec "${CONTAINER_ID}" -- sh -c "
        export DEBIAN_FRONTEND=noninteractive
        apt-get update -qq
        apt-get install -y --no-install-recommends \
            live-build \
            debootstrap \
            squashfs-tools \
            xorriso \
            isolinux \
            syslinux-common \
            grub-efi-amd64-bin \
            grub-pc-bin \
            mtools
    "
    echo "live-build toolchain installed"
fi

echo "Container ${CONTAINER_ID} ready"
