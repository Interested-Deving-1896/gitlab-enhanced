# Tools

External tools consumed as git submodules.

## linux2ipfs (`tools/linux2ipfs`)

Source: https://github.com/Jorropo/linux2ipfs

Pipelines a Linux kernel repository (or any large file tree) to IPFS by
generating CAR (Content Addressable aRchive) files using reflinks instead
of copying data. On btrfs or XFS, this is ~10x faster than `go-car` because
the file data is shared on-disk via copy-on-write.

### Role in gitlab-enhanced

`linux2ipfs` is the recommended tool for seeding large Git LFS objects and
build artefacts into the IPFS storage backend. It fits into the storage chain
as a one-shot ingestion tool, not a runtime dependency:

```
Local file tree
      │
      ▼
  linux2ipfs          ← generates CAR files via reflink (fast, no data copy)
      │
      ▼
  IPFS node (Kubo)    ← imports CAR files via /api/v0/dag/import
      │
      ▼
  IPFSBackend         ← abstraction/storage/ipfs.go reads via Kubo HTTP API
      │
      ▼
  Chain backend       ← abstraction/storage/registry.go: local → ipfs → cloud
```

### Requirements

- Linux kernel ≥ 5.3
- A reflinking filesystem: btrfs, XFS (with reflink enabled), or ZFS
- 64-bit kernel
- A running Kubo IPFS node (`ipfs daemon`)

### Build

```bash
cd tools/linux2ipfs
go build ./...
```

The binary is output to `tools/linux2ipfs/linux2ipfs` (or the module's default
output path — check `go build -v ./...` output).

### Usage: seed LFS objects to IPFS

```bash
# 1. Start your IPFS node
ipfs daemon &

# 2. Build linux2ipfs
cd tools/linux2ipfs && go build ./... && cd -

# 3. Generate a CAR file from a directory of LFS objects
tools/linux2ipfs/linux2ipfs -driver car -concurrent-chunkers 8 \
  /var/lib/gitlab-enhanced/lfs

# 4. Import the CAR into your local IPFS node
ipfs dag import *.car

# 5. Pin the root CID so it isn't garbage-collected
ipfs pin add <root-cid>
```

### Enabling the IPFS backend

In `config/local.yaml`:

```yaml
storage:
  backend: chain        # local first, then IPFS
  path: /var/lib/gitlab-enhanced/data

ipfs:
  enabled: true
  node: http://localhost:5001   # Kubo API
  gateway: http://localhost:8080
```

The `chain` backend tries the local backend first (fast reads), then falls
through to IPFS for objects not found locally. Writes always go to the first
available backend (local).

To write directly to IPFS only:

```yaml
storage:
  backend: ipfs

ipfs:
  enabled: true
  node: http://localhost:5001
```

### Filesystem requirements for reflinking

```bash
# btrfs — reflinks enabled by default
mkfs.btrfs /dev/sdX
mount /dev/sdX /var/lib/gitlab-enhanced

# XFS — must enable reflinks at format time
mkfs.xfs -m reflink=1 /dev/sdX
mount /dev/sdX /var/lib/gitlab-enhanced
```

ZFS uses copy-on-write natively; no special flags needed.
