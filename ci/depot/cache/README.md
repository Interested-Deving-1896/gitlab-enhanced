# Depot Cache

Depot's build cache is automatically used when building with `depot build` — no
separate cache configuration is needed. Depot stores layer cache on its own
infrastructure and reuses it across builds and branches.

## Cache behaviour

- **Automatic**: Depot manages cache keys based on Dockerfile layer hashes
- **Cross-branch**: Cache is shared across branches (unlike GitHub Actions cache)
- **Multi-platform**: Separate cache entries for amd64 and arm64 layers
- **Retention**: Cache entries are retained for 14 days by default

## Explicit cache export (optional)

To export the build cache to your own registry for use outside Depot:

```yaml
depot build \
  --cache-to type=registry,ref=registry.gitlab.local/cache/workspace-full \
  --cache-from type=registry,ref=registry.gitlab.local/cache/workspace-full \
  runtime/incus/images/workspace/
```

This is only needed if you want to use the cache with plain `docker buildx` or
`buildctl` outside of Depot.
