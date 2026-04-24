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
| Network filter | adblock-proxy (Rust) | Ansible | `utils/adblock-proxy/` |
| BAT rewards | rewards service (Go) | manual (opt-in) | `rewards/` |
| Bandwidth proxy | bandwidth service (Go) | manual (opt-in) | `bandwidth/` |
| Persistence | SQLite (modernc.org/sqlite) | shared by rewards + bandwidth | `store/` |
| K8s add-ons | local-path-provisioner + ingress-nginx | Ansible | `runtime/k8s-in-incus/` |

## Optional services

Three services are disabled by default and must be explicitly enabled in `config/local.yaml`.

### adblock-proxy (`adblock.enabled: true`)

A Rust HTTP sidecar embedding `brave/adblock-rust`. Loads EasyList/uBlock Origin
filter lists and exposes `POST /check` for URL filtering. Called by the Incus
runner executor to filter outbound CI job requests and by the workspace Nginx
proxy. Deployed via `deploy/ansible/adblock.yml`. Filter lists are refreshed
weekly by a systemd timer.

### BAT Rewards (`rewards.enabled: true`)

An opt-in Go HTTP service that receives GitLab system hook webhooks and queues
BAT (Basic Attention Token) tips for contributors. Reward triggers: merged MR
(1.0 BAT), closed issue (0.25 BAT), successful pipeline (0.1 BAT), repository
star (0.05 BAT). Contributors register their ERC-20 wallet address once via
`POST /wallet/register`. Payout is triggered manually via `gitlab-enhanced
rewards payout`, which submits transactions through the Uphold REST API
(client-credentials OAuth2 + committed transaction). Wallet registrations,
pending rewards, and payout history are persisted to SQLite so state survives
restarts. Incoming webhooks are validated against a configured secret token.

### Bandwidth proxy (`bandwidth.enabled: true`)

A Go HTTP reverse proxy in front of GitLab that provides:
1. **Gzip compression** — buffers upstream responses (up to 32 MiB), inspects
   Content-Type, compresses only text/JSON/JS/XML payloads (skips binary LFS
   transfers). Responses exceeding the buffer cap pass through uncompressed.
2. **LFS deduplication** — SHA-256 content-addressed hardlinks. When two
   repositories store the same large file, the second copy is replaced with a
   hardlink to the first, saving disk space.
3. **CI artifact policies** — configurable per-artifact size limit and
   retention TTL, enforced by a background goroutine every 6 hours. Artifact
   records are persisted to SQLite so the enforcer survives restarts.

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
│   └── chain   — ordered fallback for reads; fan-out writes to all backends
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

store/          — shared SQLite persistence (WAL mode, auto-migration)
                  used by rewards/ and bandwidth/ for durable state
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
