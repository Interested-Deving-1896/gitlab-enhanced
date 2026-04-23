package runner_test

import (
	"testing"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/config"
	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/runner"
)

func TestFromConfig_IncusDefault(t *testing.T) {
	cfg := &config.Config{}
	r, err := runner.FromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r == nil {
		t.Fatal("expected non-nil runner")
	}
	if r.Name() == "" {
		t.Error("expected non-empty Name()")
	}
}

func TestFromConfig_IncusExplicit(t *testing.T) {
	cfg := &config.Config{
		Build: config.BuildConfig{Socket: "/var/lib/incus/unix.socket"},
		Runner: config.RunnerConfig{
			Backend:   "incus",
			VMProfile: "gitlab-runner",
		},
		Environment: config.EnvironmentConfig{Network: "gitlab-enhanced"},
	}
	r, err := runner.FromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r == nil {
		t.Fatal("expected non-nil runner")
	}
}

func TestFromConfig_BlacksmithRequiresCloudEnabled(t *testing.T) {
	cfg := &config.Config{
		Runner: config.RunnerConfig{Backend: "blacksmith", Org: "myorg", Token: "tok"},
		Cloud:  config.CloudConfig{Enabled: false},
	}
	_, err := runner.FromConfig(cfg)
	if err == nil {
		t.Error("expected error when cloud.enabled=false for blacksmith backend")
	}
}

func TestFromConfig_BlacksmithWithCloudEnabled(t *testing.T) {
	cfg := &config.Config{
		Runner: config.RunnerConfig{Backend: "blacksmith", Org: "myorg", Token: "tok"},
		Cloud:  config.CloudConfig{Enabled: true},
	}
	r, err := runner.FromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r == nil {
		t.Fatal("expected non-nil runner")
	}
}

func TestFromConfig_UnknownBackend(t *testing.T) {
	cfg := &config.Config{
		Runner: config.RunnerConfig{Backend: "github-actions"},
	}
	_, err := runner.FromConfig(cfg)
	if err == nil {
		t.Error("expected error for unknown backend")
	}
}

func TestIncusRunner_Name(t *testing.T) {
	r := runner.NewIncusRunner("/var/lib/incus/unix.socket", "gitlab-runner", "incusbr0")
	if r.Name() == "" {
		t.Error("expected non-empty Name()")
	}
}

func TestBlacksmithRunner_Name(t *testing.T) {
	r := runner.NewBlacksmithRunner("myorg", "mytoken")
	if r.Name() == "" {
		t.Error("expected non-empty Name()")
	}
}
