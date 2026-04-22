package storage_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/storage"
)

func TestChain_UsesFirstAvailable(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	ctx := context.Background()

	// Both backends available — should use first
	b1 := storage.NewLocalBackend(dir1)
	b2 := storage.NewLocalBackend(dir2)
	chain := storage.Chain{b1, b2}

	data := []byte("chain test")
	if err := chain.Put(ctx, "key.txt", bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("chain Put: %v", err)
	}

	// Should be in b1, not b2
	if _, err := b1.Stat(ctx, "key.txt"); err != nil {
		t.Errorf("expected object in first backend: %v", err)
	}
	if _, err := b2.Stat(ctx, "key.txt"); err == nil {
		t.Error("expected object NOT in second backend")
	}
}

func TestChain_FallsBackToSecond(t *testing.T) {
	dir2 := t.TempDir()
	ctx := context.Background()

	// First backend unavailable (nonexistent path)
	b1 := storage.NewLocalBackend("/nonexistent/path")
	b2 := storage.NewLocalBackend(dir2)
	chain := storage.Chain{b1, b2}

	if !chain.Available(ctx) {
		t.Fatal("chain should be available when second backend is available")
	}

	data := []byte("fallback test")
	if err := chain.Put(ctx, "fallback.txt", bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("chain Put with fallback: %v", err)
	}

	// Should be in b2
	rc, _, err := chain.Get(ctx, "fallback.txt")
	if err != nil {
		t.Fatalf("chain Get: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, data) {
		t.Errorf("fallback content: got %q, want %q", got, data)
	}
}

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
