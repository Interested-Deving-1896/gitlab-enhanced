#!/usr/bin/env bash
# cleanup.sh — custom executor cleanup stage.
#
# Called after every job (success or failure). Force-stops the ephemeral
# container; Incus deletes it automatically because it was launched with
# --ephemeral.
#
# This script must not fail — GitLab ignores the exit code but a crash here
# can leave orphaned containers. All errors are logged and suppressed.

currentDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${currentDir}/base.sh"

echo "Cleaning up container: ${CONTAINER_ID}"

# Force-stop triggers Incus to delete the ephemeral container.
# The || true ensures we exit 0 even if the container is already gone.
incus stop --force "${CONTAINER_ID}" 2>/dev/null || true

echo "Cleanup complete"
