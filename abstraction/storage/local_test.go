package storage_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/storage"
)

func TestLocalBackend_PutGetDelete(t *testing.T) {
	dir := t.TempDir()
	b := storage.NewLocalBackend(dir)
	ctx := context.Background()

	if !b.Available(ctx) {
		t.Fatal("expected backend to be available")
	}

	// Put
	content := []byte("hello gitlab-enhanced")
	if err := b.Put(ctx, "test/object.txt", bytes.NewReader(content), int64(len(content))); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Stat
	obj, err := b.Stat(ctx, "test/object.txt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if obj.Size != int64(len(content)) {
		t.Errorf("Stat size: got %d, want %d", obj.Size, len(content))
	}
	if obj.Key != "test/object.txt" {
		t.Errorf("Stat key: got %q, want %q", obj.Key, "test/object.txt")
	}

	// Get
	rc, obj2, err := b.Get(ctx, "test/object.txt")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, content) {
		t.Errorf("Get content: got %q, want %q", got, content)
	}
	if obj2.Size != int64(len(content)) {
		t.Errorf("Get size: got %d, want %d", obj2.Size, len(content))
	}

	// Delete
	if err := b.Delete(ctx, "test/object.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Get after delete should return ErrNotFound
	_, _, err = b.Get(ctx, "test/object.txt")
	if err != storage.ErrNotFound {
		t.Errorf("Get after delete: got %v, want ErrNotFound", err)
	}

	// Delete non-existent key should be a no-op
	if err := b.Delete(ctx, "test/nonexistent.txt"); err != nil {
		t.Errorf("Delete non-existent: got %v, want nil", err)
	}
}

func TestLocalBackend_List(t *testing.T) {
	dir := t.TempDir()
	b := storage.NewLocalBackend(dir)
	ctx := context.Background()

	keys := []string{
		"lfs/objects/aa/bb/aabb1234",
		"lfs/objects/aa/cc/aacc5678",
		"lfs/objects/bb/dd/bbdd9012",
	}
	for _, k := range keys {
		data := []byte(k)
		if err := b.Put(ctx, k, bytes.NewReader(data), int64(len(data))); err != nil {
			t.Fatalf("Put %s: %v", k, err)
		}
	}

	// List with prefix
	objects, err := b.List(ctx, "lfs/objects/aa")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(objects) != 2 {
		t.Errorf("List count: got %d, want 2", len(objects))
	}

	// List all
	all, err := b.List(ctx, "lfs")
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("List all count: got %d, want 3", len(all))
	}

	// List empty prefix returns nothing (dir doesn't exist)
	none, err := b.List(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("List nonexistent: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("List nonexistent count: got %d, want 0", len(none))
	}
}

func TestLocalBackend_Unavailable(t *testing.T) {
	b := storage.NewLocalBackend("/nonexistent/path/that/does/not/exist")
	if b.Available(context.Background()) {
		t.Error("expected backend to be unavailable for nonexistent path")
	}
}

func TestLocalBackend_PutCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	b := storage.NewLocalBackend(dir)
	ctx := context.Background()

	// Deep nested key — parent dirs should be created automatically
	key := "a/b/c/d/e/object.bin"
	data := []byte("nested")
	if err := b.Put(ctx, key, bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("Put nested: %v", err)
	}

	// Verify file exists on disk
	if _, err := os.Stat(filepath.Join(dir, "a/b/c/d/e/object.bin")); err != nil {
		t.Errorf("file not found on disk: %v", err)
	}
}

func TestLocalBackend_PutIsAtomic(t *testing.T) {
	dir := t.TempDir()
	b := storage.NewLocalBackend(dir)
	ctx := context.Background()

	// Write initial content
	initial := []byte("initial")
	if err := b.Put(ctx, "atomic.txt", bytes.NewReader(initial), int64(len(initial))); err != nil {
		t.Fatalf("initial Put: %v", err)
	}

	// Overwrite
	updated := []byte("updated content")
	if err := b.Put(ctx, "atomic.txt", bytes.NewReader(updated), int64(len(updated))); err != nil {
		t.Fatalf("overwrite Put: %v", err)
	}

	rc, _, err := b.Get(ctx, "atomic.txt")
	if err != nil {
		t.Fatalf("Get after overwrite: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, updated) {
		t.Errorf("overwrite content: got %q, want %q", got, updated)
	}
}
