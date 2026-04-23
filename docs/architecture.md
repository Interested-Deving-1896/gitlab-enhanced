# Architecture

## Overview

gitlab-enhanced is a monorepo that assembles a self-hosted software development
platform from composable components. The design principle is **local-first**:
everything runs on a single machine using Incus system containers and VMs, with
cloud providers as an optional secondary layer for storage and compute.

```
┌─────────────────────────────────────────────────────────────────┐
│  Developer machine (or bare-metal server)                       │
│                                                                 │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐  │
│  │  GitLab      │  │  Soft Serve  │  │  BuildKit VM         │  │
│  │  (Omnibus)   │  │  (git host)  │  │  (image builds)      │  │
│  │  Incus ctr   │  │  systemd svc │  │  Incus VM            │  │
│  └──────────────┘  └──────────────┘  └──────────────────────┘  │
│                                                                 │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  Workspace containers (one per developer / CI job)       │   │
│  │  Incus containers — gitlab-enhanced/workspace-full image │   │
│  │  OpenVSCode Server on port 3000 (proxied to host)        │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                 │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐  │
│  │  LFS server  │  │  IPFS node   │  │  gitlab-runner       │  │
│  │  (rudolfs)   │  │  (Kubo)      │  │  (Incus executor)    │  │
│  │  systemd svc │  │  systemd svc │  │  systemd svc         │  │
│  └──────────────┘  └──────────────┘  └──────────────────────┘  │
│                                                                 │
│  Network: gitlab-enhanced-br0 (10.200.0.1/24)                  │
└─────────────────────────────────────────────────────────────────┘
                              │
                    (optional cloud layer)
                              │
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
           AWS S3           GCS           Azure Blob
        (object store)  (object store)  (object store)
```

## Component map

| Component | Technology | Managed by | Directory |
|---|---|---|---|
| Git hosting (primary) | GitLab Omnibus | Ansible | `core/omnibus/` |
| Git hosting (lightweight) | Soft Serve | Ansible | `hosting/soft-serve/` |
| Container/VM runtime | Incus | CLI / Ansible | `runtime/incus/` |
| Image builder | BuildKit in Incus VM | `abstraction/build` | `runtime/incus/images/` |
| Workspace environments | Incus containers | `abstraction/environment` | `abstraction/environment/` |
| IDE | OpenVSCode Server | systemd in container | `environments/` |
| LFS storage | rudolfs / giftless | Ansible | `lfs/` |
| Object storage | gocloud.dev/blob | `abstraction/storage` | `abstraction/storage/` |
| IPFS storage | Kubo + linux2ipfs | `abstraction/storage` | `ipfs/` |
| CI runner | gitlab-runner (Incus executor) | Ansible | `runtime/incus/runner/` |
| K8s cluster | kubeadm in Incus VMs | Ansible | `runtime/k8s-in-incus/` |
| Gitpod Classic | Helm on K8s | Ansible | `runtime/k8s-in-incus/` |
| CLI | Go + Cobra | — | `cmd/gitlab-enhanced/` |
| Cloud infra | Terraform | manual | `deploy/terraform/` |

## Abstraction layer

All backends implement a common Go interface registered via `FromConfig()`. The
active backend is selected from `config.yaml` at runtime — no recompilation needed.

```
abstraction/
├── config/     — layered YAML loader (defaults → local → cloud → env vars)
├── storage/    — Backend interface: Put/Get/Delete/List
│   ├── local   — local filesystem
│   ├── cloud   — gocloud.dev/blob (S3, GCS, Azure, MinIO, Ceph, R2)
│   ├── ipfs    — Kubo HTTP API
│   └── chain   — ordered fallback across multiple backends
├── build/      — Builder interface: Build(context, spec) → image ref
│   ├── incus   — BuildKit inside an Incus VM
│   └── depot   — Depot remote build service
├── runner/     — Runner interface: RunJob(spec) → result
│   ├── incus   — ephemeral Incus containers per CI job
│   └── blacksmith — Blacksmith remote runner API
└── environment/ — Manager interface: Create/Get/List/Stop/Start/Delete
    ├── incus   — Incus containers with OpenVSCode Server
    ├── gitpod-k8s — Gitpod Classic on K8s
    └── ona     — Ona (Gitpod Flex) API
```

## Configuration layering

Configuration is merged in this order (later values override earlier):

1. `config/defaults.yaml` — committed, safe defaults for local use
2. `config/local.yaml` — gitignored, machine-specific overrides
3. `config/cloud.yaml` — gitignored, cloud provider credentials
4. Environment variables — `GITLAB_ENHANCED_<SECTION>_<KEY>` (uppercase, underscores)

Example: `GITLAB_ENHANCED_GITLAB_DOMAIN=gitlab.example.com` overrides `gitlab.domain`.

## Network topology

All Incus instances attach to the `gitlab-enhanced-br0` bridge (10.200.0.0/24).
The host is the gateway at 10.200.0.1. Instances get DHCP addresses in
10.200.0.100–10.200.0.254.

Workspace IDE ports are forwarded to the host via Incus proxy devices:
- Each workspace gets a deterministic host port in the range 10000–59999
- Port is derived from `10000 + hash(container_name) % 50000`
- The `IDEURL` field in the `Environment` struct contains the full URL

## Hybrid monorepo structure

Owned code lives as regular directories. Upstream projects are integrated as:
- **git subtrees** — for projects we may patch (LFS servers, IPFS tools, etc.)
- **git submodules** — for upstream projects consumed without modification

Subtrees are updated with `git subtree pull --prefix <path> <remote> <branch> --squash`.
Submodules are updated with `git submodule update --remote --merge`.
