# IPFS Abstraction

Defines the interface for IPFS-backed storage and content-addressing operations
used by the `abstraction/storage` `IPFSBackend`.

## Interface

```go
// Node represents a running IPFS node that can store and retrieve content.
type Node interface {
    // Add stores data and returns its CID.
    Add(ctx context.Context, r io.Reader) (cid string, err error)
    // Get retrieves content by CID.
    Get(ctx context.Context, cid string) (io.ReadCloser, error)
    // Pin pins a CID so it is not garbage-collected.
    Pin(ctx context.Context, cid string) error
    // Unpin removes a pin.
    Unpin(ctx context.Context, cid string) error
    // Stat returns the size and type of a CID.
    Stat(ctx context.Context, cid string) (NodeStat, error)
}

type NodeStat struct {
    CID        string
    Size       uint64
    NumLinks   int
    BlockSize  uint64
}
```

## Implementations

| Implementation | Directory           | Notes                                      |
|----------------|---------------------|--------------------------------------------|
| Kubo HTTP API  | (inline in storage) | Uses the Kubo (go-ipfs) HTTP RPC API       |
| Helia          | `ipfs/helia/`       | JS implementation, submodule               |
| brig           | `ipfs/brig/`        | Encrypted IPFS filesystem, subtree         |

## CID ↔ LFS OID mapping

The `IPFSBackend` in `abstraction/storage/ipfs.go` maps Git LFS OIDs to IPFS
CIDs using a local index stored at `~/.local/share/gitlab-enhanced/ipfs-index.db`.

The `tools/linux2ipfs` submodule provides a tool for bulk-seeding LFS objects
to IPFS using reflink-based CAR generation, which is significantly faster than
adding files one at a time via the Kubo API.

## Configuration

```yaml
# config/local.yaml
storage:
  backend: ipfs
  ipfs_api: http://127.0.0.1:5001   # Kubo HTTP API
  ipfs_gateway: http://127.0.0.1:8080
```
