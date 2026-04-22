package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/config"
)

func TestLoad_DefaultsOnly(t *testing.T) {
	root := t.TempDir()
	cfgDir := filepath.Join(root, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a minimal defaults.yaml
	defaults := `
gitlab:
  domain: "test.local"
  edition: "ce"
storage:
  backend: local
  path: /tmp/test-storage
build:
  backend: incus
runner:
  backend: incus
  concurrent: 2
lfs:
  server: rudolfs
  backend: local
environment:
  backend: incus
  ide_port: 3000
cloud:
  enabled: false
`
	if err := os.WriteFile(filepath.Join(cfgDir, "defaults.yaml"), []byte(defaults), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.GitLab.Domain != "test.local" {
		t.Errorf("domain: got %q, want %q", cfg.GitLab.Domain, "test.local")
	}
	if cfg.Storage.Backend != "local" {
		t.Errorf("storage backend: got %q, want %q", cfg.Storage.Backend, "local")
	}
	if cfg.Cloud.Enabled {
		t.Error("cloud should be disabled by default")
	}
	if cfg.Runner.Concurrent != 2 {
		t.Errorf("runner concurrent: got %d, want 2", cfg.Runner.Concurrent)
	}
}

func TestLoad_LocalOverridesDefaults(t *testing.T) {
	root := t.TempDir()
	cfgDir := filepath.Join(root, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	defaults := `
gitlab:
  domain: "default.local"
storage:
  backend: local
  path: /tmp/default
cloud:
  enabled: false
`
	local := `
gitlab:
  domain: "override.local"
storage:
  path: /tmp/override
`
	if err := os.WriteFile(filepath.Join(cfgDir, "defaults.yaml"), []byte(defaults), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "local.yaml"), []byte(local), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.GitLab.Domain != "override.local" {
		t.Errorf("domain: got %q, want %q", cfg.GitLab.Domain, "override.local")
	}
	if cfg.Storage.Path != "/tmp/override" {
		t.Errorf("storage path: got %q, want %q", cfg.Storage.Path, "/tmp/override")
	}
	// Backend from defaults should be preserved
	if cfg.Storage.Backend != "local" {
		t.Errorf("storage backend: got %q, want %q", cfg.Storage.Backend, "local")
	}
}

func TestLoad_CloudOverlayNotAppliedWhenDisabled(t *testing.T) {
	root := t.TempDir()
	cfgDir := filepath.Join(root, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	defaults := `
storage:
  backend: local
  path: /tmp/local
cloud:
  enabled: false
`
	cloud := `
storage:
  backend: s3
  bucket: my-bucket
cloud:
  enabled: true
`
	if err := os.WriteFile(filepath.Join(cfgDir, "defaults.yaml"), []byte(defaults), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "cloud.yaml"), []byte(cloud), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Cloud overlay should NOT be applied because cloud.enabled=false in defaults
	if cfg.Storage.Backend != "local" {
		t.Errorf("storage backend should remain local when cloud disabled, got %q", cfg.Storage.Backend)
	}
}

func TestLoad_EnvVarOverride(t *testing.T) {
	root := t.TempDir()
	cfgDir := filepath.Join(root, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	defaults := `
storage:
  backend: local
cloud:
  enabled: false
`
	if err := os.WriteFile(filepath.Join(cfgDir, "defaults.yaml"), []byte(defaults), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GITLAB_ENHANCED_STORAGE_BACKEND", "chain")
	t.Setenv("GITLAB_ENHANCED_GITLAB_DOMAIN", "env-override.local")

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Storage.Backend != "chain" {
		t.Errorf("storage backend from env: got %q, want %q", cfg.Storage.Backend, "chain")
	}
	if cfg.GitLab.Domain != "env-override.local" {
		t.Errorf("domain from env: got %q, want %q", cfg.GitLab.Domain, "env-override.local")
	}
}

func TestLoad_MissingLocalYamlIsOK(t *testing.T) {
	root := t.TempDir()
	cfgDir := filepath.Join(root, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	defaults := `
storage:
  backend: local
cloud:
  enabled: false
`
	if err := os.WriteFile(filepath.Join(cfgDir, "defaults.yaml"), []byte(defaults), 0o644); err != nil {
		t.Fatal(err)
	}
	// No local.yaml — should not error

	_, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load without local.yaml: %v", err)
	}
}
