# Local-First Deployment

gitlab-enhanced is designed to run entirely on a single bare-metal machine
with no cloud account required. This document covers the local-first path
from a fresh machine to a fully operational GitLab stack.

## What "local-first" means

Every component has a local default:

| Component | Local default | Cloud alternative |
|---|---|---|
| Storage | Local filesystem | S3 / GCS / Azure / R2 |
| LFS | rudolfs (local) | giftless (S3-backed) |
| Build | BuildKit in Incus VM | Depot |
| Runner | Incus containers | Blacksmith |
| Environments | Incus + OpenVSCode Server | Gitpod Classic / Ona |
| IPFS | Kubo node (local) | Pinning service |

You can start with all-local and migrate individual components to cloud
backends later without changing anything else.

## Prerequisites

- Ubuntu 24.04 LTS (bare-metal or VM with nested virtualisation)
- 8 GB RAM minimum (16 GB recommended for K8s path)
- 50 GB free disk
- `sudo` access

## Bootstrap

```bash
# Clone the repo
git clone https://gitlab.com/openos-project/git-management_deving/gitlab-enhanced
cd gitlab-enhanced

# Run the bootstrap script — installs Incus, Go, Ansible, rudolfs, soft-serve
sudo bash deploy/local/bootstrap.sh
```

The bootstrap script:
1. Installs system dependencies (Incus, Go 1.25, Ansible, rudolfs, soft-serve)
2. Initialises Incus with a bridge network (`incusbr0`)
3. Creates the default storage pool
4. Copies `config/local.yaml.example` → `config/local.yaml` if absent

## Configure

Edit `config/local.yaml` (gitignored — never committed):

```yaml
gitlab:
  domain: "gitlab.local"

storage:
  path: /data/gitlab-enhanced

lfs:
  path: /data/gitlab-enhanced/lfs
```

See [configuration.md](configuration.md) for all options.

## Deploy GitLab

```bash
# Deploy GitLab CE via Ansible
ansible-playbook -i deploy/ansible/inventory/hosts.ini deploy/ansible/gitlab.yml

# Deploy LFS server
ansible-playbook -i deploy/ansible/inventory/hosts.ini deploy/ansible/lfs.yml

# Deploy Soft Serve (git mirror)
ansible-playbook -i deploy/ansible/inventory/hosts.ini deploy/ansible/soft-serve.yml
```

## Start optional services

```bash
# Bandwidth proxy (compression + LFS dedup + artifact policies)
gitlab-enhanced bandwidth serve

# BAT rewards (opt-in — requires rewards.enabled: true in local.yaml)
gitlab-enhanced rewards serve

# Adblock proxy
ansible-playbook -i deploy/ansible/inventory/hosts.ini deploy/ansible/adblock.yml
```

## Verify

```bash
gitlab-enhanced status
```

Expected output:

```
✓ gitlab      ce        https://gitlab.local
✓ lfs         rudolfs   http://127.0.0.1:8080
✓ soft-serve  soft-serve ssh://127.0.0.1:23231
✓ runner      incus     —
✓ build       incus     —
✓ environment incus     —
✓ storage     local     /data/gitlab-enhanced
```

## Add a GitLab Runner

```bash
# Register an Incus-based runner
gitlab-enhanced runner register \
  --url https://gitlab.local \
  --token glrt-xxxxxxxxxxxx
```

## Next steps

- [Cloud secondary](cloud-secondary.md) — offload storage or builds to cloud
- [Docker → Incus migration](incus-migration.md) — migrate from Docker-based GitLab
- [Architecture](architecture.md) — understand how components fit together
