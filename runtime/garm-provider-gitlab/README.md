# garm-provider-gitlab

GARM external provider that creates ephemeral GitLab CI runners using Incus
containers, registered via the GitLab JIT (just-in-time) runner token API.

## Status

**Scaffold / work in progress.** The provider interface is fully implemented
but GitLab JIT runner token support requires GitLab >= 16.0 and a personal
access token with `create_runner` scope. GARM itself does not yet have native
GitLab support — this provider bridges the gap by handling GitLab runner
registration internally.

## How it works

```
GARM (pool manager)
  │
  │  GARM_COMMAND=CreateInstance (via subprocess)
  ▼
garm-provider-gitlab
  │
  ├─ 1. Request a JIT runner token from GitLab API
  │      POST /api/v4/user/runners
  │      → { token: "glrt-...", token_expires_at: "..." }
  │
  ├─ 2. Launch an ephemeral Incus container
  │      incus launch <image> <name> --profile gitlab-runner
  │
  ├─ 3. Inject cloud-init / user-data that:
  │      a. Installs gitlab-runner
  │      b. Calls gitlab-runner register --token <jit-token> --ephemeral
  │      c. Calls gitlab-runner run (exits after one job)
  │
  └─ 4. Return ProviderInstance to GARM
         GARM polls GetInstance until runner appears online in GitLab
```

On job completion, the ephemeral runner deregisters itself. GARM calls
`DeleteInstance` to destroy the Incus container.

## Configuration

`/etc/garm/providers.d/gitlab.toml`:

```toml
[provider]
name        = "gitlab-incus"
description = "Ephemeral GitLab runners via Incus"
provider_type = "external"

[provider.external]
provider_executable = "/usr/local/bin/garm-provider-gitlab"
config_file         = "/etc/garm/providers.d/gitlab-provider.toml"
```

`/etc/garm/providers.d/gitlab-provider.toml`:

```toml
# GitLab instance URL
gitlab_url = "https://gitlab.com"

# Personal access token with create_runner scope
# Generate at: GitLab → User Settings → Access Tokens
gitlab_token = "glpat-xxxxxxxxxxxxxxxxxxxx"

# GitLab project or group to register runners against
# Use project_id for project-level runners, group_id for group-level
project_id = 12345678
# group_id = 87654321  # alternative: group-level runner

# Incus socket path on the runner host
incus_socket = "/var/lib/incus/unix.socket"

# Incus profile to apply to runner containers
incus_profile = "bdfs-privileged"

# Base Incus image for runner containers
incus_image = "ubuntu:24.04"

# Runner tags passed to GitLab during JIT registration
runner_tags = ["privileged", "self-hosted", "incus"]
```

## Building

```bash
cd runtime/garm-provider-gitlab
go build -o garm-provider-gitlab .
sudo install -m 0755 garm-provider-gitlab /usr/local/bin/
```

## Limitations

- GARM does not have a GitLab webhook receiver — runner lifecycle events
  (job started, job completed) are polled via the GitLab API rather than
  pushed. This adds ~5–30 s latency to runner teardown.
- JIT tokens expire after 1 hour. If the runner container takes longer than
  1 hour to start (unlikely), registration will fail.
- Group-level runners require the token owner to be a group Owner or Maintainer.
