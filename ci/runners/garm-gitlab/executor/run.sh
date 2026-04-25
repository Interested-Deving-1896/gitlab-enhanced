#!/usr/bin/env bash
# run.sh — custom executor run stage.
#
# Called for each script step in the job (before_script, script,
# after_script). GitLab passes the script file path as $1.
#
# Exit codes:
#   0  — success
#   1  — system failure
#   2  — build failure

currentDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${currentDir}/base.sh"
set -eo pipefail

SCRIPT_FILE="${1}"

if [ -z "${SCRIPT_FILE}" ]; then
    echo "ERROR: run.sh requires the script file path as \$1" >&2
    exit 1
fi

if [ ! -f "${SCRIPT_FILE}" ]; then
    echo "ERROR: script file not found: ${SCRIPT_FILE}" >&2
    exit 1
fi

# Stream the script into the container via stdin so we don't need to copy
# the file into the container first.
incus exec "${CONTAINER_ID}" -- bash < "${SCRIPT_FILE}"
