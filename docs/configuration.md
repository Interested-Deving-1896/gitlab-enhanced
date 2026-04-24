# Configuration Reference

Configuration is loaded from YAML files and environment variables. See
`abstraction/config/loader.go` for the full struct definitions.

## File locations

| File | Purpose | Committed |
|---|---|---|
| `config/defaults.yaml` | Safe defaults for local use | ✅ Yes |
| `config/local.yaml` | Machine-specific overrides | ❌ No (gitignored) |
| `config/cloud.yaml` | Cloud provider credentials | ❌ No (gitignored) |
| `config/local.yaml.example` | Template for local.yaml | ✅ Yes |

Copy `config/local.yaml.example` to `config/local.yaml` to get started.

## Environment variable overrides

Any config value can be overridden with an environment variable:

```
GITLAB_ENHANCED_<SECTION>_<KEY>=value
```

Examples:
```bash
GITLAB_ENHANCED_GITLAB_DOMAIN=gitlab.example.com
GITLAB_ENHANCED_STORAGE_BACKEND=cloud
GITLAB_ENHANCED_ENVIRONMENT_BACKEND=incus
```

## Full reference

### `gitlab`

```yaml
gitlab:
  domain: gitlab.local          # hostname for the GitLab instance
  admin_email: admin@gitlab.local
  admin_password: ""            # set via env var or cloud.yaml
  edition: ce                   # ce | ee
```

### `storage`

```yaml
storage:
  backend: local                # local | cloud | ipfs | chain

  # local backend
  path: /data/gitlab-enhanced/objects

  # cloud backend (requires cloud.yaml or env vars)
  provider: ""                  # aws | gcp | azure | minio | ceph | r2
  bucket: ""
  region: ""
  endpoint: ""                  # for S3-compatible providers (MinIO, Ceph, R2)

  # ipfs backend
  ipfs_api: http://127.0.0.1:5001
  ipfs_gateway: http://127.0.0.1:8080

  # chain backend — ordered list of backends; reads return the first hit,
  # writes go to ALL backends in the list (fan-out)
  backends: []
```

### `build`

```yaml
build:
  backend: incus                # incus | depot
  buildkit_version: v0.15.1    # used when creating the BuildKit VM

  # depot backend
  depot_project_id: ""
  # depot_token: set via GITLAB_ENHANCED_BUILD_DEPOT_TOKEN
```

### `runner`

```yaml
runner:
  backend: incus                # incus | blacksmith
  image: ubuntu:24.04           # base image for CI job containers
  network: gitlab-enhanced-br0

  # blacksmith backend
  blacksmith_api_url: https://api.blacksmith.sh
  # blacksmith_token: set via GITLAB_ENHANCED_RUNNER_BLACKSMITH_TOKEN
```

### `environment`

```yaml
environment:
  backend: incus                # incus | gitpod-k8s | ona
  workspace_image: "gitlab-enhanced/workspace-full:latest"
  ide: openvscode-server        # openvscode-server | jetbrains-idea | jetbrains-goland
  ide_port: 3000
  network: gitlab-enhanced-br0
  # token: set via GITLAB_ENHANCED_ENVIRONMENT_TOKEN (for gitpod-k8s and ona)
  gitpod_domain: ""             # only for gitpod-k8s; defaults to gitpod.<gitlab.domain>
```

### `lfs`

```yaml
lfs:
  backend: rudolfs              # rudolfs | giftless | lfs-test-server
  path: /data/gitlab-enhanced/lfs
  encryption: false             # rudolfs only; requires lfs_encryption_key
  # lfs_encryption_key: set via GITLAB_ENHANCED_LFS_LFS_ENCRYPTION_KEY
```

### `cloud`

```yaml
cloud:
  enabled: false                # master switch — no cloud API calls unless true
  provider: ""                  # aws | gcp | azure
```

### `registry`

```yaml
registry:
  backend: local                # local (GitLab built-in) | external
  url: registry.gitlab.local
```

### `adblock`

```yaml
adblock:
  enabled: false                # set true to start adblock-proxy sidecar
  listen_addr: "127.0.0.1:6060"
  lists_dir: "/etc/adblock-proxy/lists"
  filter_ci: true               # filter outbound CI job network requests
  filter_workspaces: true       # filter workspace container traffic
```

Environment variable overrides:
- `GITLAB_ENHANCED_ADBLOCK_ENABLED=true`
- `GITLAB_ENHANCED_ADBLOCK_LISTEN_ADDR=127.0.0.1:6060`
- `GITLAB_ENHANCED_ADBLOCK_LISTS_DIR=/etc/adblock-proxy/lists`

### `rewards`

```yaml
rewards:
  enabled: false                # MUST be explicitly true — nothing activates otherwise
  publisher_id: ""              # Brave publisher verification ID
  wallet_address: ""            # ERC-20 wallet address for receiving BAT
  uphold_client_id: ""          # Uphold OAuth2 client ID for custodial payouts
  uphold_client_secret: ""      # set via GITLAB_ENHANCED_REWARDS_UPHOLD_CLIENT_SECRET
  min_payout_bat: 5.0           # minimum BAT balance before auto-payout triggers
  listen_addr: "127.0.0.1:6061"
  webhook_secret: ""            # must match the secret set in GitLab Admin > System Hooks
  db_path: ""                   # SQLite file; defaults to /var/lib/gitlab-enhanced/rewards.db
  bandwidth_addr: ""            # address of bandwidth service for artifact auto-registration
```

Environment variable overrides:
- `GITLAB_ENHANCED_REWARDS_ENABLED=true`
- `GITLAB_ENHANCED_REWARDS_PUBLISHER_ID=...`
- `GITLAB_ENHANCED_REWARDS_WALLET_ADDRESS=0x...`
- `GITLAB_ENHANCED_REWARDS_UPHOLD_CLIENT_ID=...`
- `GITLAB_ENHANCED_REWARDS_UPHOLD_CLIENT_SECRET=...`
- `GITLAB_ENHANCED_REWARDS_LISTEN_ADDR=127.0.0.1:6061`
- `GITLAB_ENHANCED_REWARDS_WEBHOOK_SECRET=...`
- `GITLAB_ENHANCED_REWARDS_DB_PATH=/var/lib/gitlab-enhanced/rewards.db`
- `GITLAB_ENHANCED_REWARDS_BANDWIDTH_ADDR=127.0.0.1:6062`
- `GITLAB_ENHANCED_REWARDS_MIN_PAYOUT_BAT=5.0`

Wallet registrations, pending rewards, and payout history are persisted to
SQLite (`db_path`). State survives service restarts.

Payouts are submitted via the Uphold REST API when `uphold_client_id` and
`uphold_client_secret` are set. Without credentials `POST /rewards/payout`
returns an error — it does not silently mark rewards as paid.

Incoming webhooks are validated against `webhook_secret` using constant-time
comparison (`X-Gitlab-Token` header). Requests with a wrong or missing token
are rejected with HTTP 401. A startup warning is logged when no secret is set.

### `bandwidth`

```yaml
bandwidth:
  enabled: false                # set true to start bandwidth proxy
  listen_addr: "127.0.0.1:6062"
  upstream_gitlab: ""           # defaults to http://<gitlab.domain>
  compression_level: 6          # gzip level 1-9 (6 = good balance)
  lfs_dedup_enabled: true       # hardlink identical LFS objects across repos
  artifact_max_size_mb: 0       # 0 = unlimited
  artifact_retention_days: 0    # 0 = keep forever
  cache_max_size_gb: 0          # 0 = unlimited CI cache size
  db_path: ""                   # SQLite file; defaults to /var/lib/gitlab-enhanced/bandwidth.db
```

Environment variable overrides:
- `GITLAB_ENHANCED_BANDWIDTH_ENABLED=true`
- `GITLAB_ENHANCED_BANDWIDTH_LISTEN_ADDR=127.0.0.1:6062`
- `GITLAB_ENHANCED_BANDWIDTH_UPSTREAM_GITLAB=http://gitlab.local`
- `GITLAB_ENHANCED_BANDWIDTH_COMPRESSION_LEVEL=6`
- `GITLAB_ENHANCED_BANDWIDTH_ARTIFACT_MAX_SIZE_MB=500`
- `GITLAB_ENHANCED_BANDWIDTH_ARTIFACT_RETENTION_DAYS=30`
- `GITLAB_ENHANCED_BANDWIDTH_CACHE_MAX_SIZE_GB=50`
- `GITLAB_ENHANCED_BANDWIDTH_DB_PATH=/var/lib/gitlab-enhanced/bandwidth.db`

Artifact records are persisted to SQLite (`db_path`) so the retention enforcer
survives restarts. Responses larger than 32 MiB are passed through uncompressed
to prevent unbounded memory growth.

### `ipfs`

```yaml
ipfs:
  node: http://127.0.0.1:5001   # Kubo HTTP API
  gateway: http://127.0.0.1:8080
```

## Cloud credentials

Cloud credentials should never be committed. Set them in `config/cloud.yaml`
(gitignored) or via environment variables.

### AWS

```yaml
# config/cloud.yaml
storage:
  provider: aws
  bucket: my-lfs-bucket
  region: us-east-1
# Credentials via standard AWS credential chain:
# ~/.aws/credentials, IAM instance profile, or:
# AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY env vars
```

### GCP

```yaml
storage:
  provider: gcp
  bucket: my-lfs-bucket
  region: us-central1
# Credentials via GOOGLE_APPLICATION_CREDENTIALS env var or
# Application Default Credentials (gcloud auth application-default login)
```

### Azure

```yaml
storage:
  provider: azure
  bucket: my-lfs-container
  endpoint: https://mystorageaccount.blob.core.windows.net
# Credentials via AZURE_STORAGE_ACCOUNT + AZURE_STORAGE_KEY env vars
# or Azure Managed Identity
```

### MinIO / Ceph / Cloudflare R2

```yaml
storage:
  provider: minio               # or ceph, r2
  bucket: my-bucket
  region: us-east-1             # any value for MinIO/Ceph
  endpoint: http://minio.local:9000
# Credentials via AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY
```
