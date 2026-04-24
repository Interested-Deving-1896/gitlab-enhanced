# Docker → Incus Migration

This guide covers migrating an existing Docker-based GitLab installation to
the Incus-native stack used by gitlab-enhanced.

## Why migrate

| | Docker | Incus |
|---|---|---|
| Isolation | Namespace-based | VM-level (KVM) |
| Nested CI | Requires DinD (privileged) | Native VM-per-job |
| Snapshots | Volume snapshots only | Full VM snapshots |
| Live migration | Not supported | Supported |
| Resource limits | cgroups | cgroups + hardware |

## Before you start

- Back up your GitLab data: repositories, database, uploads, LFS objects
- Note your current GitLab version (`gitlab-rake gitlab:env:info`)
- Ensure the target machine has Incus installed (`gitlab-enhanced init` or `bootstrap.sh`)
- Plan a maintenance window — migration requires GitLab downtime

## Step 1 — Export data from Docker

```bash
# Stop GitLab
docker stop gitlab

# Export the GitLab backup
docker exec gitlab gitlab-backup create STRATEGY=copy

# Copy the backup archive out of the container
docker cp gitlab:/var/opt/gitlab/backups/. /tmp/gitlab-backups/

# Export secrets (required for decrypting the database)
docker cp gitlab:/etc/gitlab/gitlab-secrets.json /tmp/gitlab-secrets.json
docker cp gitlab:/etc/gitlab/gitlab.rb /tmp/gitlab.rb
```

## Step 2 — Export LFS objects

If you use GitLab's built-in LFS storage:

```bash
docker cp gitlab:/var/opt/gitlab/gitlab-rails/shared/lfs-objects/. /tmp/lfs-objects/
```

## Step 3 — Deploy GitLab in Incus

```bash
# Initialise gitlab-enhanced (creates Incus profiles and network)
gitlab-enhanced init

# Deploy GitLab CE via Ansible
ansible-playbook -i deploy/ansible/inventory/hosts.ini deploy/ansible/gitlab.yml \
  -e gitlab_domain=gitlab.local
```

## Step 4 — Restore the backup

```bash
# Copy backup into the GitLab Incus container
incus file push /tmp/gitlab-backups/*.tar /gitlab/var/opt/gitlab/backups/

# Copy secrets
incus file push /tmp/gitlab-secrets.json /gitlab/etc/gitlab/gitlab-secrets.json

# Run the restore
incus exec gitlab -- gitlab-backup restore BACKUP=<timestamp>_gitlab_backup

# Reconfigure
incus exec gitlab -- gitlab-ctl reconfigure
incus exec gitlab -- gitlab-ctl restart
```

## Step 5 — Migrate LFS to rudolfs

```bash
# Deploy rudolfs LFS server
ansible-playbook -i deploy/ansible/inventory/hosts.ini deploy/ansible/lfs.yml

# Copy LFS objects to the rudolfs store
rsync -av /tmp/lfs-objects/ /var/lib/gitlab-enhanced/lfs/

# Update GitLab to use the external LFS server
incus exec gitlab -- gitlab-rails runner "
  ApplicationSetting.current.update!(
    lfs_enabled: true,
    lfs_storage_path: nil
  )
"
```

## Step 6 — Verify

```bash
gitlab-enhanced status

# Check GitLab health
incus exec gitlab -- gitlab-rake gitlab:check
incus exec gitlab -- gitlab-rake gitlab:lfs:check_integrity
```

## Step 7 — Remove Docker

Once you've verified everything works:

```bash
docker rm -f gitlab
docker volume rm gitlab-config gitlab-logs gitlab-data
```

## Rollback

If something goes wrong, the Docker container is still available until you
remove it in Step 7. Restart it with:

```bash
docker start gitlab
```

## Common issues

**`gitlab-secrets.json` mismatch** — The secrets file must match the backup
exactly. If you get decryption errors, ensure you copied the secrets from the
same container that created the backup.

**LFS objects missing** — Run `gitlab-rake gitlab:lfs:check_integrity` to
identify missing objects and copy them from the backup.

**Runner registration** — Re-register runners after migration. Runner tokens
are stored in the database and survive the backup/restore, but the runner
agent configuration on each runner machine needs to point to the new URL.

## Next steps

- [Local-first deployment](local-first.md) — full local stack setup
- [Configuration reference](configuration.md) — all config options
- [Operations](operations.md) — day-to-day operational tasks
