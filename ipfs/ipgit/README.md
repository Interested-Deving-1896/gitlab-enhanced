# ipgit

A tool for storing Git repositories on IPFS. Packs a git repository into a
CAR (Content Addressable aRchive) file and pins it to IPFS, enabling
content-addressed, decentralised repository hosting.

## Source

This is a git subtree of:
https://github.com/ipfs-shipyard/ipgit

To pull the subtree:

```bash
git subtree add \
  --prefix ipfs/ipgit \
  https://github.com/ipfs-shipyard/ipgit.git main \
  --squash
```

## Build

Once the subtree is pulled:

```bash
cd ipfs/ipgit
go build -o bin/ipgit .
sudo install -m 755 bin/ipgit /usr/local/bin/
```

## Usage

```bash
# Push a repository to IPFS
ipgit push /path/to/repo

# Clone from IPFS CID
ipgit clone /ipfs/QmXxx... /path/to/destination

# Mirror a GitLab repository to IPFS
ipgit mirror https://gitlab.local/mygroup/myrepo.git
```

## Role in gitlab-enhanced

ipgit provides a decentralised backup and distribution mechanism for git
repositories. Combined with `ipfs-sync` for LFS objects, the full repository
history (commits + large files) can be stored on IPFS.

Use cases:
- Air-gapped distribution of repositories via IPFS CIDs
- Immutable snapshots of repository state at release time
- Decentralised mirrors for open-source projects

## Integration

The `gitlab-enhanced status` command can report the IPFS CID of the latest
repository snapshot when ipgit is configured. Add to `cmd_status.go` checks.
