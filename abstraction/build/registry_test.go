package build_test

import (
	"testing"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/build"
	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/config"
)

func TestFromConfig_IncusDefault(t *testing.T) {
	cfg := &config.Config{}
	b, err := build.FromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil builder")
	}
	if b.Name() == "" {
		t.Error("expected non-empty Name()")
	}
}

func TestFromConfig_IncusExplicit(t *testing.T) {
	cfg := &config.Config{
		Build: config.BuildConfig{
			Backend:   "incus",
			Socket:    "/var/lib/incus/unix.socket",
			CachePool: "default",
		},
	}
	b, err := build.FromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil builder")
	}
}

func TestFromConfig_DepotRequiresCloudEnabled(t *testing.T) {
	cfg := &config.Config{
		Build: config.BuildConfig{Backend: "depot", ProjectID: "proj-123", Token: "tok"},
		Cloud: config.CloudConfig{Enabled: false},
	}
	_, err := build.FromConfig(cfg)
	if err == nil {
		t.Error("expected error when cloud.enabled=false for depot backend")
	}
}

func TestFromConfig_DepotWithCloudEnabled(t *testing.T) {
	cfg := &config.Config{
		Build: config.BuildConfig{Backend: "depot", ProjectID: "proj-123", Token: "tok"},
		Cloud: config.CloudConfig{Enabled: true},
	}
	b, err := build.FromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil builder")
	}
}

func TestFromConfig_UnknownBackend(t *testing.T) {
	cfg := &config.Config{
		Build: config.BuildConfig{Backend: "circleci"},
	}
	_, err := build.FromConfig(cfg)
	if err == nil {
		t.Error("expected error for unknown backend")
	}
}

func TestIncusBuilder_Name(t *testing.T) {
	b := build.NewIncusBuilder("/var/lib/incus/unix.socket", "default")
	name := b.Name()
	if name == "" {
		t.Error("expected non-empty Name()")
	}
}

func TestDepotBuilder_Name(t *testing.T) {
	b := build.NewDepotBuilder("proj-abc", "token-xyz")
	name := b.Name()
	if name == "" {
		t.Error("expected non-empty Name()")
	}
}
