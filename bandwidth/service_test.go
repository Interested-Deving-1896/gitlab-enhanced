package bandwidth

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestService(t *testing.T, cfg Config) *Service {
	t.Helper()
	cfg.Enabled = true
	if cfg.DBPath == "" {
		cfg.DBPath = filepath.Join(t.TempDir(), "bandwidth.db")
	}
	svc, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { svc.db.Close() })
	return svc
}

func TestNewService(t *testing.T) {
	svc := newTestService(t, Config{ListenAddr: "127.0.0.1:0"})
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
	svc := newTestService(t, Config{ArtifactMaxSizeMB: 10})

	// Under limit — should succeed
	err := svc.RegisterArtifact(ArtifactRecord{
		Path:      "/tmp/small.zip",
		SizeBytes: 5 * 1024 * 1024,
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Errorf("expected no error for small artifact, got: %v", err)
	}

	// Over limit — should fail
	err = svc.RegisterArtifact(ArtifactRecord{
		Path:      "/tmp/large.zip",
		SizeBytes: 20 * 1024 * 1024,
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

	svc := newTestService(t, Config{ArtifactRetentionDays: 30})

	// Register an artifact created 60 days ago
	if err := svc.RegisterArtifact(ArtifactRecord{
		Path:      artifactPath,
		SizeBytes: 4,
		CreatedAt: time.Now().AddDate(0, 0, -60),
	}); err != nil {
		t.Fatal(err)
	}

	svc.evictArtifacts()

	// File should be deleted
	if _, err := os.Stat(artifactPath); !os.IsNotExist(err) {
		t.Error("expected artifact file to be deleted")
	}

	// DB record should be gone
	var count int
	_ = svc.db.QueryRow(`SELECT COUNT(*) FROM artifacts`).Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 artifact records after eviction, got %d", count)
	}
}

func TestArtifactPersistenceAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "bandwidth.db")

	svc1 := newTestService(t, Config{DBPath: dbPath})
	if err := svc1.RegisterArtifact(ArtifactRecord{
		Path:      "/tmp/artifact.zip",
		SizeBytes: 1024,
		CreatedAt: time.Now(),
		ProjectID: 5,
		JobID:     99,
	}); err != nil {
		t.Fatal(err)
	}
	svc1.db.Close()

	svc2 := newTestService(t, Config{DBPath: dbPath})
	var count int
	_ = svc2.db.QueryRow(`SELECT COUNT(*) FROM artifacts`).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 artifact after restart, got %d", count)
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
