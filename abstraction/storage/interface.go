// Package storage defines the backend-agnostic storage interface.
// All storage backends (local, Incus volume, IPFS, S3) implement this interface.
// The active backend is selected by config.Storage.Backend.
package storage

import (
	"bytes"
	"context"
	"fmt"
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

// Chain implements a multi-backend storage strategy:
//   - Reads (Get/Stat/List) return the first hit from the first available backend.
//   - Writes (Put/Delete) fan out to ALL available backends so every backend
//     stays in sync. If any backend fails the error is returned, but writes to
//     other backends are not rolled back.
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

// Put writes to ALL available backends. The reader is consumed once into a
// buffer so each backend receives the full content.
func (c Chain) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	// Buffer the content so it can be replayed to each backend.
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("chain put: read source: %w", err)
	}

	var firstErr error
	wrote := 0
	for _, b := range c {
		if !b.Available(ctx) {
			continue
		}
		if putErr := b.Put(ctx, key, bytes.NewReader(data), int64(len(data))); putErr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("chain put to %s: %w", b.Name(), putErr)
			}
			continue
		}
		wrote++
	}
	if wrote == 0 {
		if firstErr != nil {
			return firstErr
		}
		return ErrNoBackendAvailable
	}
	return firstErr
}

func (c Chain) Get(ctx context.Context, key string) (io.ReadCloser, *Object, error) {
	b := c.active(ctx)
	if b == nil {
		return nil, nil, ErrNoBackendAvailable
	}
	return b.Get(ctx, key)
}

// Delete removes the object from ALL available backends.
func (c Chain) Delete(ctx context.Context, key string) error {
	var firstErr error
	deleted := 0
	for _, b := range c {
		if !b.Available(ctx) {
			continue
		}
		if err := b.Delete(ctx, key); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("chain delete from %s: %w", b.Name(), err)
			}
			continue
		}
		deleted++
	}
	if deleted == 0 && firstErr == nil {
		return ErrNoBackendAvailable
	}
	return firstErr
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
