#!/usr/bin/env bash
# Custom executor — run stage.
# Called for each step of the job (prepare_script, build_script, after_script).
# gitlab-runner passes the script to execute as a file via $1.
#
# $1 — path on the HOST to a temporary script file containing the step commands
# $2 — step name (prepare_script | build_script | after_script)

set -euo pipefail

SCRIPT_PATH="$1"
STEP_NAME="${2:-build_script}"
CONTAINER_NAME="ci-job-${CUSTOM_ENV_CI_JOB_ID}"
REMOTE_SCRIPT="/tmp/gitlab-runner-step-${STEP_NAME}-$$.sh"

echo "Running step '${STEP_NAME}' in container ${CONTAINER_NAME}"

# Push the script into the container
incus file push "${SCRIPT_PATH}" "${CONTAINER_NAME}${REMOTE_SCRIPT}"
incus exec "${CONTAINER_NAME}" -- chmod +x "${REMOTE_SCRIPT}"

# Execute it; preserve the exit code so gitlab-runner sees job pass/fail
incus exec \
  --env CI=true \
  --env CI_JOB_ID="${CUSTOM_ENV_CI_JOB_ID}" \
  --env CI_PROJECT_DIR="/builds" \
  --env GOPATH="/root/go" \
  --env PATH="/usr/local/go/bin:/root/go/bin:/usr/local/lib/gitlab-runner:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" \
  "${CONTAINER_NAME}" \
  -- bash "${REMOTE_SCRIPT}"
EXIT_CODE=$?

# Clean up the remote script
incus exec "${CONTAINER_NAME}" -- rm -f "${REMOTE_SCRIPT}" 2>/dev/null || true

exit $EXIT_CODE
