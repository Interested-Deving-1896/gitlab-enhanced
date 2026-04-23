package bandwidth

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewService(t *testing.T) {
	svc, err := New(Config{
		Enabled:    true,
		ListenAddr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if svc.cfg.CompressionLevel == 0 {
		t.Error("expected default compression level to be set")
	}
}

func TestDisabledService(t *testing.T) {
	_, err := New(Config{Enabled: false})
	if err == nil {
		t.Error("expected error when bandwidth disabled, got nil")
	}
}

func TestRegisterArtifactSizeLimit(t *testing.T) {
	svc, _ := New(Config{
		Enabled:           true,
		ArtifactMaxSizeMB: 10,
	})

	// Under limit — should succeed
	err := svc.RegisterArtifact(ArtifactRecord{
		Path:      "/tmp/small.zip",
		SizeBytes: 5 * 1024 * 1024, // 5 MB
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Errorf("expected no error for small artifact, got: %v", err)
	}

	// Over limit — should fail
	err = svc.RegisterArtifact(ArtifactRecord{
		Path:      "/tmp/large.zip",
		SizeBytes: 20 * 1024 * 1024, // 20 MB
		CreatedAt: time.Now(),
	})
	if err == nil {
		t.Error("expected error for oversized artifact, got nil")
	}
}

func TestEvictExpiredArtifacts(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "old-artifact.zip")
	if err := os.WriteFile(artifactPath, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	svc, _ := New(Config{
		Enabled:               true,
		ArtifactRetentionDays: 30,
	})

	// Register an artifact created 60 days ago
	svc.artifacts = []ArtifactRecord{
		{
			Path:      artifactPath,
			SizeBytes: 4,
			CreatedAt: time.Now().AddDate(0, 0, -60),
		},
	}

	svc.evictArtifacts()

	if len(svc.artifacts) != 0 {
		t.Errorf("expected 0 artifacts after eviction, got %d", len(svc.artifacts))
	}
	if _, err := os.Stat(artifactPath); !os.IsNotExist(err) {
		t.Error("expected artifact file to be deleted")
	}
}

func TestIsCompressible(t *testing.T) {
	cases := []struct {
		ct       string
		expected bool
	}{
		{"text/html; charset=utf-8", true},
		{"application/json", true},
		{"application/javascript", true},
		{"image/png", false},
		{"application/octet-stream", false},
		{"video/mp4", false},
	}
	for _, c := range cases {
		got := isCompressible(c.ct)
		if got != c.expected {
			t.Errorf("isCompressible(%q) = %v, want %v", c.ct, got, c.expected)
		}
	}
}
