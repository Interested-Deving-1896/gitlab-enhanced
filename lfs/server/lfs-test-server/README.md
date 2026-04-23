# lfs-test-server

A minimal Git LFS server written in Go, intended for development and testing.
Not suitable for production — no authentication, no encryption, no S3 support.

## Source

This is a git subtree of:
https://github.com/git-lfs/lfs-test-server

To pull the subtree:

```bash
git subtree add \
  --prefix lfs/server/lfs-test-server \
  https://github.com/git-lfs/lfs-test-server.git main \
  --squash
```

## Build

Once the subtree is pulled:

```bash
cd lfs/server/lfs-test-server
go build -o bin/lfs-test-server .
sudo install -m 755 bin/lfs-test-server /usr/local/bin/
```

## Usage

```bash
# Start on port 8080, storing objects in /tmp/lfs-objects
LFS_LISTEN=":8080" \
LFS_HOST="localhost:8080" \
LFS_CONTENTPATH="/tmp/lfs-objects" \
LFS_ADMINUSER="admin" \
LFS_ADMINPASS="admin" \
  lfs-test-server
```

## Integration with gitlab-enhanced

Use `lfs-test-server` during local development when you don't need persistence
or authentication:

```yaml
# config/local.yaml
lfs:
  backend: lfs-test-server
  path: /tmp/lfs-objects
```

`cmd_lfs.go` starts lfs-test-server when `config.lfs.backend = lfs-test-server`.
