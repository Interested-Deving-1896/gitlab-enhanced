# gitpack

A package manager for Git repositories. Installs, updates, and removes tools
distributed as Git repositories (similar to how `go install` works for Go
binaries, but for any Git-hosted project).

## Source

This is a git subtree of:
https://github.com/nicholasgasior/gitpack

To pull the subtree:

```bash
git subtree add \
  --prefix utils/gitpack \
  https://github.com/nicholasgasior/gitpack.git master \
  --squash
```

## Build

Once the subtree is pulled:

```bash
cd utils/gitpack
go build -o bin/gitpack .
sudo install -m 755 bin/gitpack /usr/local/bin/
```

## Role in gitlab-enhanced

gitpack is used in the devcontainer and workspace images to install tools that
are not available as pre-built binaries or OS packages. It provides a consistent
installation mechanism across architectures (amd64/arm64) by building from source.

## Usage

```bash
# Install a tool from a Git repository
gitpack install github.com/some/tool

# Update all installed tools
gitpack update

# List installed tools
gitpack list

# Remove a tool
gitpack remove github.com/some/tool
```

## Integration with workspace images

The workspace Dockerfile can use gitpack to install tools that need to be built
from source for the target architecture:

```dockerfile
RUN gitpack install github.com/charmbracelet/soft-serve && \
    gitpack install github.com/git-lfs/git-lfs-transfer
```
