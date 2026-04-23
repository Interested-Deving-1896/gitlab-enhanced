# Subtrees and Submodules

## Submodules (upstream, consumed without modification)

Submodules are upstream projects that are used as-is. Update them with:

```bash
git submodule update --remote --merge
```

| Path | Upstream | Purpose |
|---|---|---|
| `dev-tools/kdevelop` | KDE | IDE for C++/Qt development |
| `dev-tools/kdesrc-build` | KDE | KDE source build tool |
| `dev-tools/kdiff3` | KDE | Diff/merge tool |
| `dev-tools/kommit` | KDE | Git GUI client |
| `ipfs/helia/verified-fetch` | IPFS | JS verified fetch over IPFS |
| `ipfs/helia/remote-pinning` | IPFS | JS remote pinning client |
| `ipfs/service-worker-gateway` | IPFS | Service worker IPFS gateway |
| `ci/graphite/cli` | Graphite | Stacked diff CLI (`gt`) |
| `environments/openvscode-server` | Gitpod | Browser-based VS Code |
| `environments/browser-extension` | Gitpod | Browser extension for workspace links |
| `utils/mirrord` | MetalBear | Mirror traffic from K8s to local process |
| `tools/linux2ipfs` | Jorropo | Bulk-seed Linux files to IPFS via CAR |

## Subtrees (upstream, may be patched)

Subtrees are upstream projects embedded directly in the repo tree. They can be
patched locally and changes can be pushed back upstream. Update with:

```bash
git subtree pull --prefix <path> <remote> <branch> --squash
```

| Path | Upstream | Purpose |
|---|---|---|
| `core/omnibus` | GitLab | GitLab Omnibus packaging |
| `core/environment-toolkit` | GitLab | GitLab Environment Toolkit (Ansible/Terraform) |
| `lfs/server/rudolfs` | jasonwhite | Rust LFS server |
| `lfs/server/giftless` | datopian | Python LFS server |
| `lfs/server/lfs-test-server` | git-lfs | Go LFS server (dev only) |
| `lfs/transfer/git-lfs` | git-lfs | Official Git LFS client |
| `lfs/transfer/git-lfs-transfer` | git-lfs | SSH-based LFS transfer |
| `lfs/adapters/lfs-folderstore` | sinbad | LFS folder storage agent |
| `lfs/adapters/lfscache` | sinbad | LFS caching proxy |
| `ipfs/brig` | sahib | Encrypted IPFS filesystem |
| `ipfs/git-lfs-ipfs` | sameer | LFS transfer agent for IPFS |
| `ipfs/ipfs-sync` | TheDiscordian | Directory sync to IPFS |
| `ipfs/ipgit` | ipfs-shipyard | Git repos on IPFS |
| `hosting/soft-serve` | charmbracelet | Self-hosted Git server |
| `hosting/soft-serve-action` | charmbracelet | CI action for Soft Serve mirroring |
| `environments/supervisor` | Gitpod | Workspace supervisor binary |
| `environments/content-service` | Gitpod | Workspace content initialisation |
| `environments/workspace-images` | Gitpod | Base workspace Dockerfiles |
| `runtime/blincus` | gmacario | Incus wrapper for dev containers |
| `utils/gitpack` | nicholasgasior | Git-based package manager |
| `utils/github-parser` | nicholasgasior | GitHub API response parser |
| `utils/github-directory-downloader` | nicholasgasior | Download GitHub subdirectories |

## Pulling all subtrees

To initialise all subtrees on a fresh clone (after the subtree remotes are added):

```bash
# Add remotes (one-time setup)
git remote add subtree-rudolfs https://github.com/jasonwhite/rudolfs.git
git remote add subtree-giftless https://github.com/datopian/giftless.git
# ... etc

# Pull each subtree
git subtree pull --prefix lfs/server/rudolfs subtree-rudolfs master --squash
git subtree pull --prefix lfs/server/giftless subtree-giftless master --squash
# ... etc
```

A helper script for this is planned at `deploy/local/pull-subtrees.sh`.

## Pulling all submodules

```bash
git submodule update --init --recursive --depth 1
```

The devcontainer `on-create.sh` runs this automatically.
