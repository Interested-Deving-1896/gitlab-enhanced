# brig

An encrypted, versioned filesystem built on top of IPFS. Provides a FUSE mount
and a CLI for managing encrypted file trees with IPFS as the content-addressed
storage backend.

## Source

This is a git subtree of:
https://github.com/sahib/brig

To pull the subtree:

```bash
git subtree add \
  --prefix ipfs/brig \
  https://github.com/sahib/brig.git master \
  --squash
```

## Build

Once the subtree is pulled:

```bash
cd ipfs/brig
go build -o bin/brig ./cmd/brig/
sudo install -m 755 bin/brig /usr/local/bin/
```

## Role in gitlab-enhanced

brig provides an encrypted FUSE filesystem over IPFS, which can be used as the
storage path for LFS objects and workspace snapshots. This gives:

- Encryption at rest (AES-256-GCM)
- Content deduplication via IPFS CIDs
- Versioned history of all stored objects
- P2P sync between nodes without a central server

## Usage

```bash
# Initialise a brig repository
brig init user@gitlab.local

# Mount the filesystem
mkdir -p /mnt/brig
brig mount /mnt/brig

# Use as LFS storage path
# config/local.yaml:
#   lfs:
#     path: /mnt/brig/lfs
```

## Integration with abstraction/storage

The `IPFSBackend` can be configured to use a brig-mounted path as its local
cache, combining brig's encryption with the IPFS content-addressing:

```yaml
storage:
  backend: chain
  backends:
    - type: local
      path: /mnt/brig/objects   # encrypted via brig
    - type: ipfs
      ipfs_api: http://127.0.0.1:5001
```
