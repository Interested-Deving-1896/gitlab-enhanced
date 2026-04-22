# Incus CI Runner

Custom executor that runs each GitLab CI job in a fresh ephemeral Incus container.

## How it works

gitlab-runner's [custom executor](https://docs.gitlab.com/runner/executors/custom.html)
calls four lifecycle scripts for every job:

| Script | When called | What it does |
|---|---|---|
| `config.sh` | Once per job, before prepare | Returns driver capabilities as JSON |
| `prepare.sh` | Before the job script | Launches an Incus container, waits for it to be ready |
| `run.sh` | For each job step | Pushes the step script into the container and executes it |
| `cleanup.sh` | After the job (pass or fail) | Deletes the container |

Each container is named `ci-job-<CI_JOB_ID>` and launched with the
`gitlab-runner` Incus profile (`runtime/incus/profiles/gitlab-runner.yaml`).

## Prerequisites

- Incus installed and initialised (`incus admin init`)
- `gitlab-runner` binary installed
- The `gitlab-runner` Incus profile applied:
  ```
  incus profile create gitlab-runner < runtime/incus/profiles/gitlab-runner.yaml
  ```
- The runner host user must be in the `incus-admin` group

## Installation

Run on the runner host from the repository root:

```bash
sudo bash runtime/incus/runner/install.sh
```

This copies `config.sh`, `prepare.sh`, `run.sh`, and `cleanup.sh` to
`/usr/local/lib/gitlab-runner-incus/`.

## Runner registration

```bash
gitlab-runner register \
  --non-interactive \
  --url "https://gitlab.com" \
  --token "YOUR_RUNNER_TOKEN" \
  --executor custom \
  --custom-config-exec  /usr/local/lib/gitlab-runner-incus/config.sh \
  --custom-prepare-exec /usr/local/lib/gitlab-runner-incus/prepare.sh \
  --custom-run-exec     /usr/local/lib/gitlab-runner-incus/run.sh \
  --custom-cleanup-exec /usr/local/lib/gitlab-runner-incus/cleanup.sh \
  --tag-list "incus,self-hosted" \
  --description "gitlab-enhanced Incus runner"
```

Or copy `config.toml` to `/etc/gitlab-runner/config.toml` and fill in the token:

```bash
sudo cp runtime/incus/runner/config.toml /etc/gitlab-runner/config.toml
sudo sed -i 's/REPLACE_WITH_RUNNER_TOKEN/YOUR_TOKEN/' /etc/gitlab-runner/config.toml
sudo gitlab-runner start
```

Obtain the token from **GitLab → Project → Settings → CI/CD → Runners → New project runner**.

## Container image selection

By default `prepare.sh` launches `ubuntu:24.04` from the Incus image server.
Override per-job with a CI variable:

```yaml
go:build:
  variables:
    INCUS_IMAGE: "ubuntu:22.04"
```

Go and other toolchains are installed inside the container at job time (via
`before_script`), or you can build a custom Incus image with them pre-installed
for faster cold starts.

## Migrating from shared runners

The `.gitlab-ci.yml` jobs already carry `tags: [incus, self-hosted]`. Once this
runner is registered and online, GitLab will route tagged jobs to it
automatically. No changes to `.gitlab-ci.yml` are needed.

The `image:` keys in each job are ignored by the Incus executor but kept as
documentation of the intended environment and for fallback to shared runners
when no Incus runner is available.

## Troubleshooting

**Container not deleted after a failed job**

```bash
incus list | grep ci-job-
incus delete --force ci-job-<JOB_ID>
```

**Job hangs at "Waiting for container to be ready"**

Check cloud-init status inside the container:

```bash
incus exec ci-job-<JOB_ID> -- cloud-init status
```

Increase `BOOT_TIMEOUT` in `prepare.sh` if the host is slow to start containers.

**`go` not found inside the container**

The default `ubuntu:24.04` image does not include Go. Add a `before_script` to
install it, or build a custom Incus image. Example `before_script`:

```yaml
before_script:
  - apt-get update -qq && apt-get install -y -qq golang-go
```
