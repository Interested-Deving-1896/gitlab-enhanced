# Graphite Server

[Graphite](https://graphite.dev/) is a code review tool that adds a stacked-diff
workflow on top of GitHub. The `ci/graphite/cli` submodule contains the upstream
Graphite CLI (`gt`).

This directory is reserved for a self-hosted Graphite server configuration if
Graphite ever ships a self-hosted option. Currently Graphite is SaaS-only.

## Current usage

The Graphite CLI (`gt`) is used locally for stacked branch management:

```bash
# Install
npm install -g @withgraphite/graphite-cli

# Authenticate
gt auth --token <your-graphite-token>

# Create a stack
gt branch create feature/my-change
gt commit create -m "first change"
gt branch create feature/my-change-part-2
gt commit create -m "second change"

# Submit the stack as PRs
gt stack submit
```

## GitLab compatibility

Graphite is GitHub-native. For GitLab stacked MRs, use the `glab` CLI with
manual branch chaining, or the `git-stack` tool:

```bash
# Install git-stack
cargo install git-stack

# Push a stack of branches as separate MRs
git stack push
```

## Future

If Graphite ships a self-hosted server, this directory will contain:
- `docker-compose.yml` / Incus profile for the server
- Ansible playbook for deployment
- Nginx reverse-proxy configuration
