# git-lfs-ipfs

A Git LFS custom transfer agent that stores LFS objects on IPFS. Objects are
content-addressed by their IPFS CID, and a local index maps LFS OIDs to CIDs.

## Source

This is a git subtree of:
https://github.com/sameer/git-lfs-ipfs

To pull the subtree:

```bash
git subtree add \
  --prefix ipfs/git-lfs-ipfs \
  https://github.com/sameer/git-lfs-ipfs.git main \
  --squash
```

## Build

Once the subtree is pulled:

```bash
cd ipfs/git-lfs-ipfs
go build -o bin/git-lfs-ipfs .
sudo install -m 755 bin/git-lfs-ipfs /usr/local/bin/
```

## Configuration

```bash
# In each repository
git config lfs.customtransfer.ipfs.path git-lfs-ipfs
git config lfs.customtransfer.ipfs.args ""
git config lfs.standalonetransferagent ipfs

# Or globally
git config --global lfs.customtransfer.ipfs.path git-lfs-ipfs
git config --global lfs.standalonetransferagent ipfs
```

Requires a running Kubo (go-ipfs) daemon:

```bash
ipfs daemon &
```

## Relationship to tools/linux2ipfs

`tools/linux2ipfs` is optimised for bulk seeding of large files to IPFS using
reflink-based CAR generation. Use it for initial migration of existing LFS
objects to IPFS. Use `git-lfs-ipfs` for ongoing push/pull operations.

## Integration with abstraction/storage

The `IPFSBackend` in `abstraction/storage/ipfs.go` implements the same
OID→CID mapping as this transfer agent, so objects stored via `git push`
(using this agent) are directly accessible via the storage backend API.
