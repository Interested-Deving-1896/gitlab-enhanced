package environment_test

import (
	"testing"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/config"
	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/environment"
)

func TestFromConfig_IncusDefault(t *testing.T) {
	cfg := &config.Config{}
	m, err := environment.FromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	if m.Name() == "" {
		t.Error("expected non-empty Name()")
	}
}

func TestFromConfig_IncusExplicit(t *testing.T) {
	cfg := &config.Config{
		Build: config.BuildConfig{Socket: "/var/lib/incus/unix.socket"},
		Environment: config.EnvironmentConfig{
			Backend: "incus",
			Network: "gitlab-enhanced",
			IDEPort: 3000,
		},
	}
	m, err := environment.FromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestFromConfig_GitpodK8s(t *testing.T) {
	cfg := &config.Config{
		Environment: config.EnvironmentConfig{Backend: "gitpod-k8s"},
	}
	m, err := environment.FromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestFromConfig_OnaRequiresCloudEnabled(t *testing.T) {
	cfg := &config.Config{
		Environment: config.EnvironmentConfig{Backend: "ona", Token: "tok"},
		Cloud:       config.CloudConfig{Enabled: false},
	}
	_, err := environment.FromConfig(cfg)
	if err == nil {
		t.Error("expected error when cloud.enabled=false for ona backend")
	}
}

func TestFromConfig_OnaWithCloudEnabled(t *testing.T) {
	cfg := &config.Config{
		Environment: config.EnvironmentConfig{Backend: "ona", Token: "tok"},
		Cloud:       config.CloudConfig{Enabled: true},
	}
	m, err := environment.FromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestFromConfig_UnknownBackend(t *testing.T) {
	cfg := &config.Config{
		Environment: config.EnvironmentConfig{Backend: "heroku"},
	}
	_, err := environment.FromConfig(cfg)
	if err == nil {
		t.Error("expected error for unknown backend")
	}
}

func TestIncusManager_Name(t *testing.T) {
	m := environment.NewIncusManager("/var/lib/incus/unix.socket", "workspace-default", "incusbr0", 3000)
	if m.Name() == "" {
		t.Error("expected non-empty Name()")
	}
}

func TestOnaManager_Name(t *testing.T) {
	m := environment.NewOnaManager("mytoken")
	if m.Name() == "" {
		t.Error("expected non-empty Name()")
	}
}
