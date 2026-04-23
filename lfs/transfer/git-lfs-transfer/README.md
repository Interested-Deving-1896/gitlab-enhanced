# git-lfs-transfer

A pure SSH-based Git LFS transfer protocol implementation. Transfers LFS objects
directly over SSH without requiring an HTTP LFS server, using the
`git-lfs-transfer` protocol extension.

## Source

This is a git subtree of:
https://github.com/git-lfs/git-lfs-transfer

To pull the subtree:

```bash
git subtree add \
  --prefix lfs/transfer/git-lfs-transfer \
  https://github.com/git-lfs/git-lfs-transfer.git main \
  --squash
```

## Build

Once the subtree is pulled:

```bash
cd lfs/transfer/git-lfs-transfer
go build -o bin/git-lfs-transfer .
sudo install -m 755 bin/git-lfs-transfer /usr/local/bin/
```

## Usage

The `git-lfs-transfer` binary is invoked automatically by `git-lfs` when the
remote URL uses SSH and the server has the binary installed:

```bash
# Client: configure the remote to use SSH LFS transfer
git config lfs.url "ssh://git@gitlab.local/mygroup/myrepo.git"

# Server: install git-lfs-transfer in PATH
sudo install -m 755 bin/git-lfs-transfer /usr/local/bin/
```

## Advantages over HTTP LFS

- No separate LFS HTTP server required
- Authentication via SSH keys (same as git push/pull)
- Works through SSH tunnels and jump hosts
- Simpler firewall rules (only port 22 needed)

## Integration with Soft Serve

Soft Serve supports the `git-lfs-transfer` protocol natively when
`git-lfs-transfer` is installed on the server. No additional configuration
is needed beyond installing the binary.
