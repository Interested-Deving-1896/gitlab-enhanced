# Depot Builder Integration

[Depot](https://depot.dev/) provides remote container image builds with persistent
layer caching, native multi-platform support (amd64 + arm64 simultaneously), and
faster builds than local Docker or BuildKit.

## When to use

Use Depot when:
- You need multi-platform images (amd64 + arm64) without QEMU emulation
- Build times are dominated by layer cache misses in CI
- You want to offload heavy image builds from CI runners

For local builds, the `runtime/incus/images/workspace/build.sh` script uses
BuildKit directly and does not require Depot.

## Setup

1. Create a Depot project at https://depot.dev
2. Set `DEPOT_TOKEN` as a CI secret
3. Note your project ID (shown in the Depot dashboard)

## GitLab CI integration

```yaml
image:build:
  stage: image
  image: depot/cli:latest
  variables:
    DEPOT_PROJECT_ID: "your-project-id"
  script:
    - depot build
        --project $DEPOT_PROJECT_ID
        --platform linux/amd64,linux/arm64
        --tag registry.gitlab.local/gitlab-enhanced/workspace-full:latest
        --push
        runtime/incus/images/workspace/
  rules:
    - changes:
        - runtime/incus/images/workspace/Dockerfile
```

## abstraction/build integration

The `DepotBuilder` in `abstraction/build/depot.go` wraps the `depot build` CLI.
Configure it in `config/local.yaml`:

```yaml
build:
  backend: depot
  depot_project_id: "your-project-id"
  depot_token: ""   # set via GITLAB_ENHANCED_BUILD_DEPOT_TOKEN env var
```
