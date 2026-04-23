# LFS Abstraction

Defines the interface and registry for Git LFS server backends. The abstraction
mirrors the pattern used in `abstraction/storage` and `abstraction/environment`.

## Interface

```go
// Server is a running LFS server that can be started and stopped.
type Server interface {
    // Name returns the backend identifier (rudolfs | giftless | lfs-test-server).
    Name() string
    // Start launches the server process and blocks until it is ready.
    Start(ctx context.Context, cfg Config) error
    // Stop gracefully shuts down the server.
    Stop(ctx context.Context) error
    // URL returns the base URL the server is listening on.
    URL() string
}

// Config holds the common configuration for all LFS server backends.
type Config struct {
    // ListenAddr is the address to bind (default: 127.0.0.1:8080).
    ListenAddr string
    // StoragePath is the local directory for object storage (rudolfs, lfs-test-server).
    StoragePath string
    // S3Bucket is the S3 bucket name for cloud-backed storage (rudolfs, giftless).
    S3Bucket string
    // S3Region is the AWS region for S3-backed storage.
    S3Region string
    // AuthToken is the shared secret for LFS authentication (optional).
    AuthToken string
}
```

## Backends

| Backend         | Directory              | Storage           | Notes                        |
|-----------------|------------------------|-------------------|------------------------------|
| `rudolfs`       | `lfs/server/rudolfs/`  | local or S3       | Rust, production-grade       |
| `giftless`      | `lfs/server/giftless/` | local, S3, GCS    | Python, multi-cloud          |
| `lfs-test-server` | `lfs/server/lfs-test-server/` | local  | Go, development only         |

## Registry

The `cmd_lfs.go` `lfs serve` command selects the backend from `config.lfs.backend`
and delegates to the appropriate server implementation.

## Adapters

`lfs/adapters/` contains caching and folder-mapping layers that sit in front of
any backend:

- `lfs-folderstore/` — maps LFS OIDs to a flat directory structure
- `lfscache/` — transparent caching proxy between client and upstream LFS server
