# ipfs-sync

A tool for bidirectional synchronisation between a local directory and IPFS.
Watches a directory for changes and automatically pins new/modified files to
IPFS, maintaining a mapping of paths to CIDs.

## Source

This is a git subtree of:
https://github.com/TheDiscordian/ipfs-sync

To pull the subtree:

```bash
git subtree add \
  --prefix ipfs/ipfs-sync \
  https://github.com/TheDiscordian/ipfs-sync.git master \
  --squash
```

## Build

Once the subtree is pulled:

```bash
cd ipfs/ipfs-sync
go build -o bin/ipfs-sync .
sudo install -m 755 bin/ipfs-sync /usr/local/bin/
```

## Usage

```bash
# Sync a directory to IPFS, watching for changes
ipfs-sync \
  --db ~/.local/share/gitlab-enhanced/ipfs-sync.db \
  --endpoint http://127.0.0.1:5001 \
  /data/gitlab-enhanced/lfs
```

## Role in gitlab-enhanced

ipfs-sync can be run as a background service to automatically mirror the local
LFS object store to IPFS. This provides:

- Automatic off-site backup of all LFS objects
- Content availability via IPFS gateways
- Deduplication across repositories sharing the same objects

Run as a systemd service alongside the LFS server:

```ini
[Unit]
Description=IPFS sync for LFS objects
After=ipfs.service

[Service]
ExecStart=/usr/local/bin/ipfs-sync \
  --db /var/lib/gitlab-enhanced/ipfs-sync.db \
  --endpoint http://127.0.0.1:5001 \
  /data/gitlab-enhanced/lfs
Restart=always

[Install]
WantedBy=multi-user.target
```
