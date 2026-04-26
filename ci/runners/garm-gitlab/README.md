# garm-gitlab

Pool-based GitLab CI runner manager for [Incus](https://linuxcontainers.org/incus/) containers and VMs.

Inspired by [cloudbase/garm](https://github.com/cloudbase/garm) but targeting GitLab CI and Incus instead of GitHub Actions and cloud providers.

## How it works

1. GitLab sends a **job webhook** when a CI job enters the `pending` state.
2. garm-gitlab matches the job's tags against configured **pools**.
3. If a matching pool has no idle runner and is below `max_runners`, it provisions a new **Incus container/VM**, installs `gitlab-runner` in custom executor mode, and registers it with GitLab.
4. The runner picks up the job. Each job runs in its own ephemeral Incus container (launched by the executor scripts).
5. A background **reconcile loop** (30 s) enforces `min_idle` and scales down runners that have been idle longer than `idle_timeout`.

### Privileged containers (live-build)

Set `privileged = true` on a pool to enable `security.privileged` and `security.nesting` on job containers. This is required for Debian ISO building with `live-build`, which needs nested container support.

## Directory layout

```
garm-gitlab/
├── cmd/garm-gitlab/main.go          # binary entry point
├── internal/
│   ├── config/config.go             # TOML config schema + validation
│   ├── gitlab/
│   │   ├── webhook.go               # job webhook listener
│   │   └── runner.go                # runner registration/deregistration API
│   ├── pool/pool.go                 # pool manager + scale logic
│   └── provider/incus.go            # Incus compute backend
├── executor/
│   ├── base.sh                      # shared variables
│   ├── prepare.sh                   # launch ephemeral job container
│   ├── run.sh                       # exec job script in container
│   └── cleanup.sh                   # stop/delete job container
└── deploy/
    ├── garm-gitlab.service          # systemd unit
    └── install.sh                   # build + install script
```

## Installation

```sh
git clone <repo>
cd ci/runners/garm-gitlab
sudo ./deploy/install.sh
sudo cp /etc/garm-gitlab/config.toml.example /etc/garm-gitlab/config.toml
# edit config.toml
sudo systemctl enable --now garm-gitlab
```

## Configuration

See `deploy/install.sh` for the annotated example config written to `/etc/garm-gitlab/config.toml.example`.

Key fields:

| Field | Description |
|---|---|
| `api.listen_address` | `host:port` for the webhook HTTP server |
| `api.webhook_secret` | Must match the secret set on the GitLab webhook |
| `gitlab.url` | Base URL of your GitLab instance |
| `gitlab.token` | Personal access token with `api` scope |
| `pool.registration_token` | Runner registration token from GitLab Settings → CI/CD |
| `pool.privileged` | Enable nested containers (required for live-build) |
| `pool.min_idle` | Warm runners to keep ready at all times |
| `pool.max_runners` | Hard cap on total instances in the pool |
| `pool.idle_timeout` | How long an idle runner waits before scale-down |

## GitLab webhook setup

In your project or group: **Settings → Webhooks → Add new webhook**

- URL: `http://<garm-gitlab-host>:8080/webhook`
- Secret token: value of `api.webhook_secret`
- Trigger: **Job events** only
