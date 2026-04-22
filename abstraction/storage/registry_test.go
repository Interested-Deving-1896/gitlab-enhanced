package storage_test

import (
	"testing"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/config"
	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/storage"
)

func TestFromConfig_Local(t *testing.T) {
	cfg := &config.Config{
		Storage: config.StorageConfig{Backend: "local", Path: t.TempDir()},
	}
	b, err := storage.FromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.Name() == "" {
		t.Error("expected non-empty Name()")
	}
}

func TestFromConfig_LocalMissingPath(t *testing.T) {
	cfg := &config.Config{
		Storage: config.StorageConfig{Backend: "local"},
	}
	_, err := storage.FromConfig(cfg)
	if err == nil {
		t.Error("expected error for missing path")
	}
}

func TestFromConfig_CloudRequiresEnabled(t *testing.T) {
	cfg := &config.Config{
		Storage: config.StorageConfig{Backend: "cloud", Provider: "aws", Bucket: "b"},
		Cloud:   config.CloudConfig{Enabled: false},
	}
	_, err := storage.FromConfig(cfg)
	if err == nil {
		t.Error("expected error when cloud.enabled=false")
	}
}

func TestFromConfig_CloudRequiresProvider(t *testing.T) {
	cfg := &config.Config{
		Storage: config.StorageConfig{Backend: "cloud", Bucket: "b"},
		Cloud:   config.CloudConfig{Enabled: true},
	}
	_, err := storage.FromConfig(cfg)
	if err == nil {
		t.Error("expected error when provider is empty")
	}
}

func TestFromConfig_CloudRequiresBucket(t *testing.T) {
	cfg := &config.Config{
		Storage: config.StorageConfig{Backend: "cloud", Provider: "aws"},
		Cloud:   config.CloudConfig{Enabled: true},
	}
	_, err := storage.FromConfig(cfg)
	if err == nil {
		t.Error("expected error when bucket is empty")
	}
}

func TestFromConfig_CloudAllProviders(t *testing.T) {
	providers := []struct {
		provider string
		endpoint string
	}{
		{"aws", ""},
		{"gcs", ""},
		{"azure", ""},
		{"minio", "http://minio:9000"},
		{"ceph", "http://ceph:7480"},
		{"r2", "https://account.r2.cloudflarestorage.com"},
	}
	for _, p := range providers {
		t.Run(p.provider, func(t *testing.T) {
			cfg := &config.Config{
				Storage: config.StorageConfig{
					Backend:  "cloud",
					Provider: p.provider,
					Bucket:   "test-bucket",
					Region:   "us-east-1",
					Endpoint: p.endpoint,
				},
				Cloud: config.CloudConfig{Enabled: true},
			}
			b, err := storage.FromConfig(cfg)
			if err != nil {
				t.Fatalf("provider %q: unexpected error: %v", p.provider, err)
			}
			if b == nil {
				t.Fatalf("provider %q: got nil backend", p.provider)
			}
		})
	}
}

func TestFromConfig_ChainLocalOnly(t *testing.T) {
	cfg := &config.Config{
		Storage: config.StorageConfig{Backend: "chain", Path: t.TempDir()},
		IPFS:    config.IPFSConfig{Enabled: false},
		Cloud:   config.CloudConfig{Enabled: false},
	}
	b, err := storage.FromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil backend")
	}
}

func TestFromConfig_ChainWithCloud(t *testing.T) {
	cfg := &config.Config{
		Storage: config.StorageConfig{
			Backend:  "chain",
			Path:     t.TempDir(),
			Provider: "aws",
			Bucket:   "my-bucket",
			Region:   "us-east-1",
		},
		IPFS:  config.IPFSConfig{Enabled: false},
		Cloud: config.CloudConfig{Enabled: true},
	}
	b, err := storage.FromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil backend")
	}
}

func TestFromConfig_ChainEmpty(t *testing.T) {
	cfg := &config.Config{
		Storage: config.StorageConfig{Backend: "chain"},
		IPFS:    config.IPFSConfig{Enabled: false},
		Cloud:   config.CloudConfig{Enabled: false},
	}
	_, err := storage.FromConfig(cfg)
	if err == nil {
		t.Error("expected error for empty chain")
	}
}

func TestFromConfig_UnknownBackend(t *testing.T) {
	cfg := &config.Config{
		Storage: config.StorageConfig{Backend: "dropbox"},
	}
	_, err := storage.FromConfig(cfg)
	if err == nil {
		t.Error("expected error for unknown backend")
	}
}
