package storage

import (
	"context"
	"io"
)

// IPFSBackend stores objects on an IPFS node via the HTTP API.
// This is a stub — full implementation lives in ipfs/abstraction/.
// Enabled only when ipfs.enabled=true in config.
type IPFSBackend struct {
	nodeURL string
}

func NewIPFSBackend(nodeURL string) *IPFSBackend {
	return &IPFSBackend{nodeURL: nodeURL}
}

func (b *IPFSBackend) Name() string { return "ipfs:" + b.nodeURL }

func (b *IPFSBackend) Available(ctx context.Context) bool {
	// TODO: ping /api/v0/version on the IPFS node
	return b.nodeURL != ""
}

func (b *IPFSBackend) Put(_ context.Context, _ string, _ io.Reader, _ int64) error {
	return errNotImplemented("IPFSBackend.Put")
}

func (b *IPFSBackend) Get(_ context.Context, _ string) (io.ReadCloser, *Object, error) {
	return nil, nil, errNotImplemented("IPFSBackend.Get")
}

func (b *IPFSBackend) Delete(_ context.Context, _ string) error {
	return errNotImplemented("IPFSBackend.Delete")
}

func (b *IPFSBackend) Stat(_ context.Context, _ string) (*Object, error) {
	return nil, errNotImplemented("IPFSBackend.Stat")
}

func (b *IPFSBackend) List(_ context.Context, _ string) ([]Object, error) {
	return nil, errNotImplemented("IPFSBackend.List")
}

var _ Backend = (*IPFSBackend)(nil)
