# gitlab-enhanced

A unified monorepo combining GitLab packaging, deployment, LFS storage, IPFS transport,
dev environments, CI acceleration, and developer tooling — local-first by default,
Incus-native, cloud-optional.

## Architecture

```
gitlab-enhanced/
├── core/           GitLab packaging (omnibus) + deployment (environment-toolkit)
├── lfs/            Unified Git LFS layer: servers, clients, adapters
├── ipfs/           IPFS transport backends for git data and LFS objects
├── hosting/        SSH-first git server (soft-serve)
├── ci/             Self-hosted CI acceleration: Blacksmith, Depot, Graphite
├── environments/   Dev environment layer: OpenVSCode Server, supervisor, workspace images
├── dev-tools/      KDE developer toolchain
├── runtime/        Incus profiles, Blincus, K8s-in-Incus
├── abstraction/    Cross-cutting interfaces: storage, build, runner, environment
├── deploy/         Local (Incus) and cloud (Terraform/Ansible) deployment
├── packaging/      Omnibus package definitions
├── utils/          Shared utilities
└── config/         Layered configuration (local-first defaults, cloud overlays)
```

## Principles

- **Local-first**: every component runs on a single machine via Incus with no cloud account required
- **Cloud-secondary**: cloud providers are opt-in overlays, never defaults
- **No Docker**: Incus replaces Docker throughout — system containers, VMs, OCI-compatible
- **Modular sources**: upstream projects are git subtrees (owned/forked) or submodules (consumed)
- **Abstraction layer**: all backends sit behind stable Go interfaces; swapping is a config change

## Quick Start

```bash
# Prerequisites: Incus 6.0+, Ansible, Go 1.22+
./deploy/local/bootstrap.sh
```

## Self-hosted CI Runner

To route CI jobs to a local Incus runner instead of GitLab shared runners:

1. On a Linux host with Incus installed, get a runner token from  
   **GitLab → Project → Settings → CI/CD → Runners → New project runner**  
   (set tags: `incus, self-hosted`)

2. Register in one command:
   ```bash
   sudo gitlab-enhanced runner register --token glrt-xxxxxxxxxxxxxxxxxxxx
   ```
   This installs the executor scripts and calls `gitlab-runner register` with the
   correct custom executor flags. See [`runtime/incus/runner/README.md`](runtime/incus/runner/README.md)
   for options and troubleshooting.

3. Start the runner:
   ```bash
   sudo gitlab-runner start
   ```

4. Optionally pre-bake the CI image for faster cold starts:
   ```bash
   bash runtime/incus/images/build-ci-base.sh
   ```

Once the runner is online, the `image:rebuild-ci-base` and `image:rebuild-workspace`
CI jobs will route to it automatically — no changes to `.gitlab-ci.yml` needed.

## Source Strategy

| Type | Used for | Sync upstream |
|------|----------|---------------|
| git subtree | Projects we own or fork | `git subtree pull --prefix=<path> <remote> <branch> --squash` |
| git submodule | Upstream projects consumed without forking | `git submodule update --remote` |

## Docs

- [Architecture](docs/architecture.md)
- [Local-first deployment](docs/local-first.md)
- [Cloud secondary](docs/cloud-secondary.md)
- [Docker → Incus migration](docs/incus-migration.md)
- [Contributing](docs/contributing.md)

[![Build with Ona](https://ona.com/build-with-ona.svg)](https://app.ona.com/#https://gitlab.com/openos-project/git-management_deving/gitlab-enhanced)