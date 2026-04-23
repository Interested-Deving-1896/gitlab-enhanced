# Soft Serve

[Soft Serve](https://github.com/charmbracelet/soft-serve) is a self-hosted Git
server with a TUI, SSH access, and a built-in web UI. It serves as the local-first
Git hosting layer in gitlab-enhanced — a lightweight alternative to running full
GitLab for small or air-gapped deployments.

## Source

This is a git subtree of the upstream Soft Serve repository:
https://github.com/charmbracelet/soft-serve

To pull the subtree:

```bash
git subtree add \
  --prefix hosting/soft-serve \
  https://github.com/charmbracelet/soft-serve.git main \
  --squash
```

## Role in gitlab-enhanced

Soft Serve is deployed by `deploy/ansible/soft-serve.yml` and managed as a
systemd service on the host. It provides:

- SSH-based `git clone/push/pull` on port 23231
- Web UI for browsing repositories on port 23232
- Webhook support for triggering CI pipelines

The `gitlab-enhanced up` command deploys Soft Serve alongside GitLab Omnibus.
For minimal deployments (no GitLab), Soft Serve alone is sufficient for hosting.

## Configuration

Soft Serve is configured via `~/.config/soft-serve/config.yaml`. The Ansible
playbook writes this file from `deploy/ansible/roles/soft-serve/templates/config.yaml.j2`.

Key settings:

```yaml
name: "gitlab-enhanced"
ssh:
  listen_addr: ":23231"
  public_url: "ssh://git.{{ gitlab_domain }}:23231"
http:
  listen_addr: ":23232"
  public_url: "http://git.{{ gitlab_domain }}:23232"
initial_admin_keys:
  - "{{ lookup('file', '~/.ssh/id_ed25519.pub') }}"
```

## Usage

```bash
# Clone a repository
git clone ssh://git.gitlab.local:23231/myrepo

# Create a new repository
ssh git.gitlab.local -p 23231 repo create myrepo

# Browse the TUI
ssh git.gitlab.local -p 23231
```
