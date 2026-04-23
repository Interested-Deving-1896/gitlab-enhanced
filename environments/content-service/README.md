# Content Service

The content service handles workspace content initialisation: cloning the
repository, applying snapshots, and managing the workspace filesystem inside
a container.

## Source

This is a git subtree of the Gitpod Classic content-service:
https://github.com/gitpod-io/gitpod (path: `components/content-service`)

To pull the subtree:

```bash
git subtree add \
  --prefix environments/content-service \
  https://github.com/gitpod-io/gitpod.git main \
  --squash
```

## Role in gitlab-enhanced

In the Incus environment backend (`abstraction/environment/incus.go`), repository
cloning is handled directly via `git clone` inside the container. The content
service provides a more sophisticated alternative with:

- Snapshot-based workspace restore (faster cold starts)
- Incremental content updates
- Workspace backup to object storage

When the subtree is pulled and the binary is built, `incus.go` can delegate
content initialisation to the content-service gRPC API instead of raw `git clone`.

## Build

Once the subtree is pulled:

```bash
cd environments/content-service
go build -o bin/content-service ./cmd/content-serviceapi/
```

## Integration point

`abstraction/environment/incus.go` `cloneRepo()` method — replace the direct
`git clone` exec with a gRPC call to the content-service when available.
