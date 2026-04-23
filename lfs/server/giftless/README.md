# giftless

A Python-based Git LFS server with pluggable storage backends (local, S3, GCS,
Azure Blob). Supports the Git LFS batch API and file locking.

## Source

This is a git subtree of:
https://github.com/datopian/giftless

To pull the subtree:

```bash
git subtree add \
  --prefix lfs/server/giftless \
  https://github.com/datopian/giftless.git master \
  --squash
```

## Install

Once the subtree is pulled:

```bash
cd lfs/server/giftless
pip install -e .
# or
pip install giftless
```

## Configuration

Giftless is configured via a YAML file:

```yaml
# /etc/giftless/config.yaml
TRANSFER_ADAPTERS:
  basic:
    factory: giftless.transfer.basic_streaming:factory
    options:
      storage_class: giftless.storage.local_storage:LocalStorage
      storage_options:
        path: /data/gitlab-enhanced/lfs

AUTH_PROVIDERS:
  - factory: giftless.auth.allow_all:factory

MIDDLEWARE:
  - class: werkzeug.middleware.proxy_fix:ProxyFix
    kwargs:
      x_for: 1
      x_proto: 1
```

## Usage

```bash
# Start with local storage
GIFTLESS_CONFIG_FILE=/etc/giftless/config.yaml \
  gunicorn --bind 0.0.0.0:8080 'giftless.wsgi_app:app'
```

## Multi-cloud storage

Giftless supports multiple storage backends simultaneously via transfer adapters.
See the upstream documentation for S3, GCS, and Azure Blob configuration.

## Integration with gitlab-enhanced

`cmd_lfs.go` starts giftless when `config.lfs.backend = giftless`:

```yaml
# config/local.yaml
lfs:
  backend: giftless
  path: /data/gitlab-enhanced/lfs
```
