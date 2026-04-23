# Workspace Images

Base container images for workspace environments. These are the foundation layers
that the workspace Dockerfile (`runtime/incus/images/workspace/Dockerfile`) builds on.

## Image hierarchy

```
ubuntu:24.04  (upstream)
    └── gitlab-enhanced/workspace-base:latest
            ├── Common tools: git, curl, build-essential, ca-certificates
            ├── User setup: non-root user, sudo, locale
            └── Systemd: enabled for service management
        └── gitlab-enhanced/workspace-full:latest  (built by runtime/incus/images/workspace/)
                ├── Go 1.24
                ├── Node.js LTS
                ├── Rust (stable)
                ├── Ruby 3.3
                ├── Python 3.12
                └── OpenVSCode Server
```

## Building the base image

```bash
# Build and publish to local Incus image store
incus image build ubuntu:24.04 \
  --alias gitlab-enhanced/workspace-base:latest \
  -- \
  bash -c "
    apt-get update && apt-get install -y --no-install-recommends \
      git curl wget ca-certificates build-essential sudo locales \
      systemd systemd-sysv dbus && \
    locale-gen en_US.UTF-8 && \
    useradd -m -s /bin/bash -G sudo gitpod && \
    echo 'gitpod ALL=(ALL) NOPASSWD:ALL' >> /etc/sudoers.d/gitpod
  "
```

## Source

This directory is intended as a git subtree of Gitpod's workspace-images:
https://github.com/gitpod-io/workspace-images

To pull:

```bash
git subtree add \
  --prefix environments/workspace-images \
  https://github.com/gitpod-io/workspace-images.git main \
  --squash
```

The Gitpod workspace-images repo provides Dockerfile variants for many language
stacks. These can be used as-is or as reference for extending the local workspace
Dockerfile.
