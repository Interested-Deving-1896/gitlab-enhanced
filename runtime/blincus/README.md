# Blincus

[Blincus](https://github.com/gmacario/blincus) is a wrapper around Incus that
provides a developer-friendly CLI for launching and managing development
containers, similar to how Distrobox wraps Podman/Docker.

## Source

This is a git subtree of:
https://github.com/gmacario/blincus

To pull the subtree:

```bash
git subtree add \
  --prefix runtime/blincus \
  https://github.com/gmacario/blincus.git main \
  --squash
```

## Role in gitlab-enhanced

Blincus complements the `gitlab-enhanced env` commands by providing a simpler
interface for developers who want to launch a workspace container without going
through the full environment API. It is particularly useful for:

- Quick one-off development containers
- Testing workspace images before deploying them via the full stack
- Developers who prefer a Distrobox-style workflow

## Installation

Once the subtree is pulled:

```bash
cd runtime/blincus
make install
```

Or install the pre-built binary:

```bash
curl -fsSL https://raw.githubusercontent.com/gmacario/blincus/main/install.sh | bash
```

## Usage with gitlab-enhanced workspace images

```bash
# Launch a workspace container using the gitlab-enhanced workspace image
blincus create --image gitlab-enhanced/workspace-full:latest my-workspace

# Enter the container
blincus enter my-workspace

# List running containers
blincus list

# Delete a container
blincus delete my-workspace
```

## Relationship to `gitlab-enhanced env`

| Feature                    | `gitlab-enhanced env` | `blincus`          |
|----------------------------|-----------------------|--------------------|
| IDE proxy (OpenVSCode)     | ✅                    | ❌ (manual)        |
| Repo clone on create       | ✅                    | ❌ (manual)        |
| Config-driven              | ✅                    | ❌                 |
| Quick interactive shell    | ❌                    | ✅                 |
| Distrobox-style home mount | ❌                    | ✅                 |

Use `gitlab-enhanced env create` for full workspace environments with IDE access.
Use `blincus` for quick interactive containers.
