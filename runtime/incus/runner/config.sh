#!/usr/bin/env bash
# Custom executor — config stage.
# Called once per job before prepare. Outputs driver capabilities as JSON.
# https://docs.gitlab.com/runner/executors/custom.html#config

set -euo pipefail

cat <<'JSON'
{
  "driver": {
    "name": "gitlab-enhanced Incus executor",
    "version": "1.0.0"
  },
  "job_env": {
    "INCUS_EXECUTOR": "1"
  },
  "builds_dir": "/builds",
  "cache_dir": "/cache",
  "builds_dir_is_shared": false
}
JSON
