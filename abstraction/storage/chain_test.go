package storage_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/storage"
)

// TestChain_PutFanOut verifies that Put writes to ALL available backends.
func TestChain_PutFanOut(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	ctx := context.Background()

	b1 := storage.NewLocalBackend(dir1)
	b2 := storage.NewLocalBackend(dir2)
	chain := storage.Chain{b1, b2}

	data := []byte("fan-out test")
	if err := chain.Put(ctx, "key.txt", bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("chain Put: %v", err)
	}

	// Object must be in BOTH backends.
	if _, err := b1.Stat(ctx, "key.txt"); err != nil {
		t.Errorf("expected object in first backend: %v", err)
	}
	if _, err := b2.Stat(ctx, "key.txt"); err != nil {
		t.Errorf("expected object in second backend: %v", err)
	}
}

// TestChain_GetReturnsFirstHit verifies that Get returns the first backend's copy.
func TestChain_GetReturnsFirstHit(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	ctx := context.Background()

	b1 := storage.NewLocalBackend(dir1)
	b2 := storage.NewLocalBackend(dir2)

	// Write different content to each backend directly.
	_ = b1.Put(ctx, "key.txt", bytes.NewReader([]byte("from-b1")), 7)
	_ = b2.Put(ctx, "key.txt", bytes.NewReader([]byte("from-b2")), 7)

	chain := storage.Chain{b1, b2}
	rc, _, err := chain.Get(ctx, "key.txt")
	if err != nil {
		t.Fatalf("chain Get: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if string(got) != "from-b1" {
		t.Errorf("Get: got %q, want %q", got, "from-b1")
	}
}

// TestChain_PutSkipsUnavailableBackend verifies that Put succeeds when only
// one backend is available, writing to that backend only.
func TestChain_PutSkipsUnavailableBackend(t *testing.T) {
	dir2 := t.TempDir()
	ctx := context.Background()

	b1 := storage.NewLocalBackend("/nonexistent/path")
	b2 := storage.NewLocalBackend(dir2)
	chain := storage.Chain{b1, b2}

	data := []byte("partial fan-out")
	if err := chain.Put(ctx, "fallback.txt", bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("chain Put with one unavailable backend: %v", err)
	}

	// Must be readable via the chain (from b2).
	rc, _, err := chain.Get(ctx, "fallback.txt")
	if err != nil {
		t.Fatalf("chain Get: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, data) {
		t.Errorf("content: got %q, want %q", got, data)
	}
}

// TestChain_AllUnavailable verifies that Put and Get return ErrNoBackendAvailable
// when no backend in the chain is reachable.
func TestChain_AllUnavailable(t *testing.T) {
	ctx := context.Background()
	b1 := storage.NewLocalBackend("/nonexistent/1")
	b2 := storage.NewLocalBackend("/nonexistent/2")
	chain := storage.Chain{b1, b2}

	if chain.Available(ctx) {
		t.Error("chain should not be available when all backends are unavailable")
	}

	err := chain.Put(ctx, "key", bytes.NewReader(nil), 0)
	if err != storage.ErrNoBackendAvailable {
		t.Errorf("Put: got %v, want ErrNoBackendAvailable", err)
	}

	_, _, err = chain.Get(ctx, "key")
	if err != storage.ErrNoBackendAvailable {
		t.Errorf("Get: got %v, want ErrNoBackendAvailable", err)
	}
}

// TestChain_DeleteFanOut verifies that Delete removes the object from all backends.
func TestChain_DeleteFanOut(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	ctx := context.Background()

	b1 := storage.NewLocalBackend(dir1)
	b2 := storage.NewLocalBackend(dir2)
	chain := storage.Chain{b1, b2}

	data := []byte("to delete")
	_ = chain.Put(ctx, "del.txt", bytes.NewReader(data), int64(len(data)))

	if err := chain.Delete(ctx, "del.txt"); err != nil {
		t.Fatalf("chain Delete: %v", err)
	}

	if _, err := b1.Stat(ctx, "del.txt"); err == nil {
		t.Error("expected object to be deleted from first backend")
	}
	if _, err := b2.Stat(ctx, "del.txt"); err == nil {
		t.Error("expected object to be deleted from second backend")
	}
}
