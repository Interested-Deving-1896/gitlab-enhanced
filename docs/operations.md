# Operations

## Initial setup

### Prerequisites

- Ubuntu 24.04 LTS (or Debian 12) on the host machine
- 16 GB RAM minimum (32 GB recommended for K8s + Gitpod)
- 100 GB free disk space
- Internet access for initial package downloads

### Bootstrap

The bootstrap script installs all dependencies and builds the CLI:

```bash
curl -fsSL \
  https://gitlab.com/openos-project/git-management_deving/gitlab-enhanced/-/raw/main/deploy/local/bootstrap.sh \
  | bash
```

Or, if you have already cloned the repo:

```bash
bash deploy/local/bootstrap.sh
```

After bootstrap, add `~/bin` or the repo's `bin/` to your PATH:

```bash
export PATH="$HOME/bin:$PATH"
# or
export PATH="/path/to/gitlab-enhanced/bin:$PATH"
```

### First run

```bash
# Initialise Incus network, storage pool, and profiles
gitlab-enhanced init

# Deploy GitLab, LFS server, Soft Serve, and BuildKit
gitlab-enhanced up

# Check all components are healthy
gitlab-enhanced status
```

## Day-to-day operations

### Starting and stopping

```bash
# Stop all services (preserves data volumes)
gitlab-enhanced down

# Restart everything
gitlab-enhanced up

# Force-stop (skips graceful shutdown)
gitlab-enhanced down --force
```

### Workspace management

```bash
# Create a workspace for a repository
gitlab-enhanced env create --repo https://gitlab.local/mygroup/myrepo.git

# List all workspaces
gitlab-enhanced env list

# Open a workspace (prints the IDE URL)
gitlab-enhanced env list --json | jq -r '.[] | select(.name=="myrepo") | .ide_url'

# Stop a workspace
gitlab-enhanced env stop <id>

# Delete a workspace (removes the container, not the repo)
gitlab-enhanced env delete <id>
```

### LFS server

```bash
# Start the LFS server (uses backend from config)
gitlab-enhanced lfs serve

# Check LFS server status
gitlab-enhanced lfs status
```

### Health check

```bash
# Check all components
gitlab-enhanced status

# JSON output for scripting
gitlab-enhanced status --json
```

## Incus operations

Direct Incus commands for debugging:

```bash
# List all instances
incus list

# Get a shell in the GitLab container
incus exec gitlab-enhanced-gitlab -- bash

# View GitLab logs
incus exec gitlab-enhanced-gitlab -- gitlab-ctl tail

# View workspace IDE logs
incus exec <workspace-name> -- journalctl -u ide -f

# View BuildKit logs
incus exec buildkit -- journalctl -u buildkitd -f
```

## Updating components

### GitLab Omnibus

```bash
# Re-run the GitLab Ansible playbook
ansible-playbook -i deploy/ansible/inventory/hosts.ini deploy/ansible/gitlab.yml
```

### Workspace image

```bash
# Rebuild the workspace image (requires BuildKit instance running)
bash runtime/incus/images/workspace/build.sh

# Or trigger via CI (manual job)
# In GitLab CI: run the image:rebuild-workspace job
```

### BuildKit

Update `BUILDKIT_VERSION` in `runtime/incus/cloud-init/buildkit.yaml` and
`abstraction/build/incus.go` (`buildkitCloudInit` constant), then rebuild:

```bash
incus delete buildkit --force
gitlab-enhanced up
```

## Backup and restore

### GitLab backup

```bash
# Create a GitLab backup inside the container
incus exec gitlab-enhanced-gitlab -- gitlab-backup create

# Copy the backup to the host
incus file pull gitlab-enhanced-gitlab/var/opt/gitlab/backups/<timestamp>_gitlab_backup.tar .
```

### LFS objects

LFS objects are stored at the path configured in `config.lfs.path`
(default: `/data/gitlab-enhanced/lfs`). Back up this directory with any
standard tool (rsync, restic, etc.).

If using the `chain` storage backend with cloud as secondary, LFS objects
are automatically mirrored to the configured cloud bucket.

### Configuration

```bash
# Back up local config
cp config/local.yaml config/local.yaml.bak
cp config/cloud.yaml config/cloud.yaml.bak
```

## Optional services

### adblock-proxy

```bash
# Deploy (builds from source, downloads filter lists, installs systemd service)
ansible-playbook -i deploy/ansible/inventory/hosts.ini deploy/ansible/adblock.yml

# Check status
gitlab-enhanced status          # shows adblock row
gitlab-enhanced adblock status  # direct health check — not a subcommand; use:
curl http://127.0.0.1:6060/health

# Reload filter lists without restart
curl -X POST http://127.0.0.1:6060/reload

# Check a URL manually
curl -X POST http://127.0.0.1:6060/check \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com/ad.js","resource_type":"script"}'

# View logs
journalctl -u adblock-proxy -f
```

### BAT Rewards

```bash
# Enable in config/local.yaml first, then:
gitlab-enhanced rewards serve &

# Register a contributor wallet
gitlab-enhanced rewards wallet register \
  --username alice \
  --wallet 0xYourERC20Address

# View pending rewards
gitlab-enhanced rewards pending

# View current rates
gitlab-enhanced rewards rates

# Trigger payout
gitlab-enhanced rewards payout

# Configure GitLab system hook (Admin > System Hooks):
#   URL: http://127.0.0.1:6061/webhook/gitlab
#   Triggers: Merge request events, Issue events, Pipeline events
```

### Bandwidth proxy

```bash
# Enable in config/local.yaml first, then:
gitlab-enhanced bandwidth serve &

# View savings statistics
gitlab-enhanced bandwidth stats

# View artifact retention policy
gitlab-enhanced bandwidth policy

# Trigger immediate artifact eviction
gitlab-enhanced bandwidth evict

# Deduplicate a specific LFS object
gitlab-enhanced bandwidth dedup <oid> <size-bytes>

# View logs
journalctl -u gitlab-enhanced-bandwidth -f
```

## Troubleshooting

### GitLab not starting

```bash
incus exec gitlab-enhanced-gitlab -- gitlab-ctl status
incus exec gitlab-enhanced-gitlab -- gitlab-ctl reconfigure
```

### Workspace IDE not accessible

```bash
# Check the IDE service inside the container
incus exec <workspace-name> -- systemctl status ide
incus exec <workspace-name> -- journalctl -u ide --no-pager -n 50

# Check the proxy device
incus config show <workspace-name> | grep -A5 ide-proxy
```

### BuildKit not responding

```bash
incus exec buildkit -- systemctl status buildkitd
incus exec buildkit -- journalctl -u buildkitd --no-pager -n 50

# Test buildctl connectivity
incus exec buildkit -- buildctl debug workers
```

### Incus network issues

```bash
# Verify the bridge exists
incus network show gitlab-enhanced-br0

# Check DHCP leases
incus network list-leases gitlab-enhanced-br0

# Restart the network
incus network set gitlab-enhanced-br0 ipv4.nat true
```

### adblock-proxy not starting

```bash
# Check the binary exists
which adblock-proxy || ls /usr/local/bin/adblock-proxy

# Check filter lists exist
ls /etc/adblock-proxy/lists/

# Re-run the Ansible playbook to rebuild and reinstall
ansible-playbook -i deploy/ansible/inventory/hosts.ini deploy/ansible/adblock.yml

# Check service logs
journalctl -u adblock-proxy --no-pager -n 50
```

### Rewards service not receiving webhooks

```bash
# Verify the service is running
curl http://127.0.0.1:6061/health

# Check GitLab system hook is configured
# Admin > System Hooks — URL must be http://127.0.0.1:6061/webhook/gitlab
# Triggers: Merge request events, Issue events, Pipeline events

# Test the webhook manually
curl -X POST http://127.0.0.1:6061/webhook/gitlab \
  -H 'X-Gitlab-Event: Merge Request Hook' \
  -H 'Content-Type: application/json' \
  -d '{"object_attributes":{"state":"merged","iid":1},"user":{"username":"alice","email":"alice@example.com"}}'
```

### Bandwidth proxy not compressing

```bash
# Verify the service is running and upstream is reachable
curl http://127.0.0.1:6062/health

# Check stats — bytes_saved should be non-zero after some traffic
gitlab-enhanced bandwidth stats

# Ensure clients send Accept-Encoding: gzip
# The proxy only compresses when the client advertises gzip support
```

### CI runner not picking up jobs

```bash
# Check runner registration
gitlab-runner list

# View runner logs
journalctl -u gitlab-runner -f

# Verify the Incus executor scripts are installed
ls -la /usr/local/lib/gitlab-runner-incus/
```
