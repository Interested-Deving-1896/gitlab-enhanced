# lfs-folderstore

A Git LFS custom transfer agent that stores objects in a plain directory tree
on the local filesystem, using the standard two-level OID directory layout
(`ab/cd/<full-oid>`). Useful for NFS mounts, external drives, or any path that
is not the default `.git/lfs/objects`.

## Source

This is a git subtree of:
https://github.com/sinbad/lfs-folderstore

To pull the subtree:

```bash
git subtree add \
  --prefix lfs/adapters/lfs-folderstore \
  https://github.com/sinbad/lfs-folderstore.git master \
  --squash
```

## Build

Once the subtree is pulled:

```bash
cd lfs/adapters/lfs-folderstore
go build -o bin/lfs-folderstore .
sudo install -m 755 bin/lfs-folderstore /usr/local/bin/
```

## Configuration

In each repository that should use the folderstore:

```bash
# Set the storage path (can be a network share or external drive)
git config lfs.customtransfer.lfs-folder.path /mnt/lfs-store
git config lfs.customtransfer.lfs-folder.args ""
git config lfs.standalonetransferagent lfs-folder
```

Or globally in `~/.gitconfig`:

```ini
[lfs "customtransfer.lfs-folder"]
    path = /data/gitlab-enhanced/lfs
    args =
[lfs]
    standalonetransferagent = lfs-folder
```

## Integration with gitlab-enhanced

The `abstraction/storage` `LocalBackend` stores objects at the path configured
in `config.lfs.path`. The folderstore agent uses the same path, so objects
written by the storage backend are directly accessible to `git lfs pull`.
