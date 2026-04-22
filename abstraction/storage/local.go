package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
)

// LocalBackend stores objects as files under a root directory.
// This is the default backend — no external dependencies required.
type LocalBackend struct {
	root string
}

func NewLocalBackend(root string) *LocalBackend {
	return &LocalBackend{root: root}
}

func (b *LocalBackend) Name() string { return "local:" + b.root }

func (b *LocalBackend) Available(_ context.Context) bool {
	info, err := os.Stat(b.root)
	return err == nil && info.IsDir()
}

func (b *LocalBackend) path(key string) string {
	return filepath.Join(b.root, filepath.FromSlash(key))
}

func (b *LocalBackend) Put(_ context.Context, key string, r io.Reader, _ int64) error {
	p := b.path(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".tmp-")
	if err != nil {
		return err
	}
	defer func() {
		f.Close()
		os.Remove(f.Name()) // clean up on failure
	}()
	if _, err := io.Copy(f, r); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), p)
}

func (b *LocalBackend) Get(_ context.Context, key string) (io.ReadCloser, *Object, error) {
	p := b.path(key)
	f, err := os.Open(p)
	if os.IsNotExist(err) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return f, &Object{
		Key:     key,
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}, nil
}

func (b *LocalBackend) Delete(_ context.Context, key string) error {
	err := os.Remove(b.path(key))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (b *LocalBackend) Stat(_ context.Context, key string) (*Object, error) {
	info, err := os.Stat(b.path(key))
	if os.IsNotExist(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &Object{
		Key:     key,
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}, nil
}

func (b *LocalBackend) List(_ context.Context, prefix string) ([]Object, error) {
	base := filepath.Join(b.root, filepath.FromSlash(prefix))
	var objects []Object
	err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(b.root, path)
		objects = append(objects, Object{
			Key:     filepath.ToSlash(rel),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return objects, err
}

// Ensure LocalBackend satisfies Backend at compile time.
var _ Backend = (*LocalBackend)(nil)
