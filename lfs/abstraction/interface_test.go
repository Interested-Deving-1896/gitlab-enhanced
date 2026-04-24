package abstraction_test

import (
	"testing"

	abstraction "gitlab.com/openos-project/git-management_deving/gitlab-enhanced/lfs/abstraction"
	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/config"
)

func TestFromConfig_DefaultsToRudolfs(t *testing.T) {
	cfg := &config.Config{}
	cfg.LFS.Server = ""
	cfg.LFS.Path = "/tmp/lfs"

	srv, err := abstraction.FromConfig(cfg)
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}
	if srv.Name() != "rudolfs" {
		t.Errorf("expected rudolfs, got %q", srv.Name())
	}
}

func TestFromConfig_Giftless(t *testing.T) {
	cfg := &config.Config{}
	cfg.LFS.Server = "giftless"
	cfg.LFS.Path = "/tmp/lfs"

	srv, err := abstraction.FromConfig(cfg)
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}
	if srv.Name() != "giftless" {
		t.Errorf("expected giftless, got %q", srv.Name())
	}
}

func TestFromConfig_LFSTestServer(t *testing.T) {
	cfg := &config.Config{}
	cfg.LFS.Server = "lfs-test-server"
	cfg.LFS.Path = "/tmp/lfs"

	srv, err := abstraction.FromConfig(cfg)
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}
	if srv.Name() != "lfs-test-server" {
		t.Errorf("expected lfs-test-server, got %q", srv.Name())
	}
}

func TestFromConfig_UnknownBackend(t *testing.T) {
	cfg := &config.Config{}
	cfg.LFS.Server = "nonexistent"

	_, err := abstraction.FromConfig(cfg)
	if err == nil {
		t.Fatal("expected error for unknown backend, got nil")
	}
}

func TestExecServer_URL(t *testing.T) {
	cfg := &config.Config{}
	cfg.LFS.Server = "rudolfs"
	cfg.LFS.Path = "/tmp/lfs"

	srv, err := abstraction.FromConfig(cfg)
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}
	url := srv.URL()
	if url == "" {
		t.Error("URL() returned empty string")
	}
	if url[:7] != "http://" {
		t.Errorf("URL() should start with http://, got %q", url)
	}
}

func TestExecServer_StopWithoutStart(t *testing.T) {
	cfg := &config.Config{}
	cfg.LFS.Server = "rudolfs"
	cfg.LFS.Path = "/tmp/lfs"

	srv, err := abstraction.FromConfig(cfg)
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}
	// Stop before Start should be a no-op, not a panic.
	if err := srv.Stop(nil); err != nil {
		t.Errorf("Stop before Start: %v", err)
	}
}
