// Package storage defines the backend-agnostic storage interface.
// All storage backends (local, Incus volume, IPFS, S3) implement this interface.
// The active backend is selected by config.Storage.Backend.
package storage

import (
	"context"
	"io"
	"time"
)

// Object represents a stored blob identified by its key.
type Object struct {
	Key         string
	Size        int64
	ContentType string
	ModTime     time.Time
}

// Backend is the storage abstraction all implementations satisfy.
type Backend interface {
	// Available reports whether this backend is reachable right now.
	Available(ctx context.Context) bool

	// Put stores data under key. Overwrites if key exists.
	Put(ctx context.Context, key string, r io.Reader, size int64) error

	// Get retrieves the object at key. Caller must close the returned ReadCloser.
	Get(ctx context.Context, key string) (io.ReadCloser, *Object, error)

	// Delete removes the object at key. No-ops if key does not exist.
	Delete(ctx context.Context, key string) error

	// Stat returns metadata for key without fetching content.
	Stat(ctx context.Context, key string) (*Object, error)

	// List returns all objects whose keys share the given prefix.
	List(ctx context.Context, prefix string) ([]Object, error)

	// Name returns a human-readable identifier for this backend.
	Name() string
}

// Chain tries each backend in order, using the first one that is available.
// This implements the local-first fallback strategy.
type Chain []Backend

// Available returns true if any backend in the chain is available.
func (c Chain) Available(ctx context.Context) bool {
	for _, b := range c {
		if b.Available(ctx) {
			return true
		}
	}
	return false
}

// active returns the first available backend, or nil.
func (c Chain) active(ctx context.Context) Backend {
	for _, b := range c {
		if b.Available(ctx) {
			return b
		}
	}
	return nil
}

func (c Chain) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	b := c.active(ctx)
	if b == nil {
		return ErrNoBackendAvailable
	}
	return b.Put(ctx, key, r, size)
}

func (c Chain) Get(ctx context.Context, key string) (io.ReadCloser, *Object, error) {
	b := c.active(ctx)
	if b == nil {
		return nil, nil, ErrNoBackendAvailable
	}
	return b.Get(ctx, key)
}

func (c Chain) Delete(ctx context.Context, key string) error {
	b := c.active(ctx)
	if b == nil {
		return ErrNoBackendAvailable
	}
	return b.Delete(ctx, key)
}

func (c Chain) Stat(ctx context.Context, key string) (*Object, error) {
	b := c.active(ctx)
	if b == nil {
		return nil, ErrNoBackendAvailable
	}
	return b.Stat(ctx, key)
}

func (c Chain) List(ctx context.Context, prefix string) ([]Object, error) {
	b := c.active(ctx)
	if b == nil {
		return nil, ErrNoBackendAvailable
	}
	return b.List(ctx, prefix)
}

func (c Chain) Name() string { return "chain" }
