#!/usr/bin/env bash
# base.sh — shared variables sourced by all executor scripts.
#
# GitLab CI injects CUSTOM_ENV_* variables for every CI variable defined in
# the job or project settings. The ones below are either standard GitLab
# runner variables or garm-gitlab-specific ones set via the pool config.

# Unique container name derived from runner/project/job IDs so concurrent
# jobs on the same runner host never collide.
CONTAINER_ID="runner-${CUSTOM_ENV_CI_RUNNER_ID}-project-${CUSTOM_ENV_CI_PROJECT_ID}-concurrent-${CUSTOM_ENV_CI_CONCURRENT_PROJECT_ID}-${CUSTOM_ENV_CI_JOB_ID}"

# Incus image alias to launch for this job. Defaults to ubuntu:noble.
# Override per-job via a CI variable: GARM_INCUS_IMAGE: "debian:bookworm"
CONTAINER_IMAGE="${CUSTOM_ENV_GARM_INCUS_IMAGE:-ubuntu:noble}"

# Set to "true" to enable security.privileged + security.nesting on the
# ephemeral job container. Required for live-build and other nested-container
# workloads. Override per-job via: GARM_INCUS_PRIVILEGED: "true"
CONTAINER_PRIVILEGED="${CUSTOM_ENV_GARM_INCUS_PRIVILEGED:-false}"
