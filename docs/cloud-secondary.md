# Cloud Secondary

gitlab-enhanced follows a local-first model: everything runs locally by
default, and cloud services are opt-in overlays. This document explains how
to enable cloud backends for storage, builds, and runners while keeping the
rest of the stack local.

## When to use cloud secondary

- Your LFS objects exceed local disk capacity
- You want remote build caching shared across machines
- You need elastic CI capacity beyond what a single machine provides
- You want geo-redundant artifact storage

## Configuration

Cloud settings live in `config/cloud.yaml` (gitignored). Copy the example:

```bash
cp config/local.yaml.example config/cloud.yaml
```

Enable cloud mode:

```yaml
cloud:
  enabled: true
  provider: aws   # aws | gcs | azure | minio | ceph | r2
```

## Storage backends

### S3 (AWS, MinIO, Ceph, R2)

```yaml
storage:
  backend: cloud
  bucket: my-gitlab-enhanced-bucket
  region: us-east-1
```

Set credentials via environment:

```bash
export AWS_ACCESS_KEY_ID=...
export AWS_SECRET_ACCESS_KEY=...
```

For MinIO or Ceph, set the endpoint:

```yaml
cloud:
  provider: minio
  endpoint: https://minio.internal:9000
```

### Chain (local + cloud)

Keep a local copy for fast reads and replicate to cloud:

```yaml
storage:
  backend: chain
  backends:
    - local
    - cloud
```

Writes fan out to both backends. Reads return the first hit (local).

### IPFS

```yaml
ipfs:
  enabled: true
  node: http://127.0.0.1:5001   # local Kubo node
  # or point to a remote pinning service
```

## Build backends

### Depot (remote BuildKit)

```yaml
build:
  backend: depot
  project_id: your-depot-project-id
  token: dpt_...   # set via GITLAB_ENHANCED_BUILD_TOKEN
```

Depot provides remote BuildKit with shared cache across machines and CI.

## Runner backends

### Blacksmith (elastic CI)

```yaml
cloud:
  enabled: true

runner:
  backend: blacksmith
  org: your-org
  token: ...   # set via GITLAB_ENHANCED_RUNNER_TOKEN
  # Optional: self-hosted Blacksmith scheduler
  blacksmith_api_url: https://blacksmith.internal
```

## Rewards payouts via Uphold

```yaml
rewards:
  enabled: true
  uphold_client_id: ...      # set via GITLAB_ENHANCED_REWARDS_UPHOLD_CLIENT_ID
  uphold_client_secret: ...  # set via GITLAB_ENHANCED_REWARDS_UPHOLD_CLIENT_SECRET
```

## Environment variable reference

All cloud credentials should be set via environment variables rather than
committed to `cloud.yaml`. See [configuration.md](configuration.md) for the
full list of `GITLAB_ENHANCED_*` overrides.

## Verify

```bash
gitlab-enhanced status
```

Cloud-backed components show their backend name in the status output.

## Next steps

- [Local-first deployment](local-first.md) — start here if you haven't already
- [Configuration reference](configuration.md) — all config options
