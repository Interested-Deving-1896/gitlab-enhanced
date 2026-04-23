# lfscache

A transparent caching proxy for Git LFS. Sits between `git-lfs` clients and an
upstream LFS server, caching downloaded objects locally to avoid repeated
downloads of the same large files across multiple clones or CI jobs.

## Source

This is a git subtree of:
https://github.com/sinbad/lfscache

To pull the subtree:

```bash
git subtree add \
  --prefix lfs/adapters/lfscache \
  https://github.com/sinbad/lfscache.git master \
  --squash
```

## Build

Once the subtree is pulled:

```bash
cd lfs/adapters/lfscache
go build -o bin/lfscache .
sudo install -m 755 bin/lfscache /usr/local/bin/
```

## Usage

```bash
# Start the cache proxy (listens on :9999 by default)
lfscache --upstream https://gitlab.local/mygroup/myrepo.git/info/lfs \
         --cache-dir /data/gitlab-enhanced/lfs-cache \
         --listen :9999

# Configure git to use the cache
git config lfs.url http://localhost:9999
```

## Integration with gitlab-enhanced

In CI environments (Incus runner containers), lfscache can be run as a sidecar
service to share the LFS cache across all jobs on the same host. Configure the
runner's `prepare.sh` to start lfscache before the job and set `GIT_LFS_URL`
to point to the local cache.

The `runtime/incus/runner/prepare.sh` script can be extended to start lfscache:

```bash
# In prepare.sh, after container is ready:
if command -v lfscache &>/dev/null; then
  lfscache --upstream "${CI_SERVER_URL}/${CI_PROJECT_PATH}.git/info/lfs" \
           --cache-dir /var/cache/lfscache \
           --listen :9999 &
  incus exec "${CONTAINER_NAME}" -- git config --global lfs.url http://10.200.0.1:9999
fi
```
