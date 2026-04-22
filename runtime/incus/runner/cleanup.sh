#!/usr/bin/env bash
# Custom executor — cleanup stage.
# Called after every job (pass or fail). Deletes the ephemeral container.
# Failure here is non-fatal — gitlab-runner logs it but does not fail the job.

set -euo pipefail

CONTAINER_NAME="ci-job-${CUSTOM_ENV_CI_JOB_ID}"

if incus info "${CONTAINER_NAME}" &>/dev/null; then
  echo "Deleting container ${CONTAINER_NAME}"
  incus delete --force "${CONTAINER_NAME}"
  echo "Container ${CONTAINER_NAME} deleted"
else
  echo "Container ${CONTAINER_NAME} already gone — nothing to clean up"
fi
