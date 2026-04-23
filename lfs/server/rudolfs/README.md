# rudolfs

A high-performance Git LFS server written in Rust with local disk and S3 storage
backends. Production-grade, low memory footprint, supports encryption at rest.

## Source

This is a git subtree of:
https://github.com/jasonwhite/rudolfs

To pull the subtree:

```bash
git subtree add \
  --prefix lfs/server/rudolfs \
  https://github.com/jasonwhite/rudolfs.git master \
  --squash
```

## Build

Once the subtree is pulled:

```bash
cd lfs/server/rudolfs
cargo build --release
sudo install -m 755 target/release/rudolfs /usr/local/bin/
```

Or install from crates.io:

```bash
cargo install rudolfs
```

## Usage

### Local storage

```bash
rudolfs --storage local \
        --cache-dir /data/gitlab-enhanced/lfs \
        --host 0.0.0.0:8080
```

### S3 storage

```bash
rudolfs --storage s3 \
        --s3-bucket my-lfs-bucket \
        --s3-region us-east-1 \
        --host 0.0.0.0:8080
```

### With encryption

```bash
# Generate a key
rudolfs key generate > /etc/rudolfs/key

rudolfs --storage local \
        --cache-dir /data/gitlab-enhanced/lfs \
        --key /etc/rudolfs/key \
        --host 0.0.0.0:8080
```

## Integration with gitlab-enhanced

`cmd_lfs.go` starts rudolfs when `config.lfs.backend = rudolfs`:

```yaml
# config/local.yaml
lfs:
  backend: rudolfs
  path: /data/gitlab-enhanced/lfs
```

The Ansible playbook `deploy/ansible/lfs.yml` installs and configures rudolfs
as a systemd service.

## GitLab configuration

In GitLab Omnibus (`/etc/gitlab/gitlab.rb`):

```ruby
gitlab_rails['lfs_enabled'] = true
gitlab_rails['lfs_storage_path'] = "/data/gitlab-enhanced/lfs"
# Or point to the rudolfs server:
# gitlab_rails['lfs_object_store_enabled'] = true
# gitlab_rails['lfs_object_store_remote_directory'] = "lfs-objects"
```
