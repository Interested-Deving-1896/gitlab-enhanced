// SPDX-License-Identifier: Apache-2.0
// Package config holds the provider configuration loaded from the TOML file
// pointed to by GARM_PROVIDER_CONFIG_FILE.
package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Config is the provider-level configuration for garm-provider-gitlab.
// It is loaded once at startup from the file named by GARM_PROVIDER_CONFIG_FILE.
type Config struct {
	// GitLabURL is the base URL of the GitLab instance.
	// Default: https://gitlab.com
	GitLabURL string `toml:"gitlab_url"`

	// GitLabToken is a personal access token with the create_runner scope.
	// Generate at: GitLab → User Settings → Access Tokens → create_runner
	GitLabToken string `toml:"gitlab_token"`

	// ProjectID registers runners at the project level.
	// Mutually exclusive with GroupID.
	ProjectID int64 `toml:"project_id"`

	// GroupID registers runners at the group level.
	// Mutually exclusive with ProjectID.
	GroupID int64 `toml:"group_id"`

	// IncusSocket is the path to the Incus Unix socket on the runner host.
	// Default: /var/lib/incus/unix.socket
	IncusSocket string `toml:"incus_socket"`

	// IncusProfile is the Incus profile applied to runner containers.
	// Use "bdfs-privileged" for integration tests requiring losetup/BTRFS.
	// Default: gitlab-runner
	IncusProfile string `toml:"incus_profile"`

	// IncusImage is the base image for runner containers.
	// Default: ubuntu:24.04
	IncusImage string `toml:"incus_image"`

	// RunnerTags are the GitLab CI tags applied to JIT-registered runners.
	// Jobs must carry matching tags to route to these runners.
	RunnerTags []string `toml:"runner_tags"`
}

// NewConfig loads and validates the provider configuration from path.
func NewConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var cfg Config
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if c.GitLabToken == "" {
		return fmt.Errorf("gitlab_token is required")
	}
	if c.ProjectID == 0 && c.GroupID == 0 {
		return fmt.Errorf("one of project_id or group_id is required")
	}
	if c.ProjectID != 0 && c.GroupID != 0 {
		return fmt.Errorf("project_id and group_id are mutually exclusive")
	}
	if c.GitLabURL == "" {
		c.GitLabURL = "https://gitlab.com"
	}
	if c.IncusSocket == "" {
		c.IncusSocket = "/var/lib/incus/unix.socket"
	}
	if c.IncusProfile == "" {
		c.IncusProfile = "gitlab-runner"
	}
	if c.IncusImage == "" {
		c.IncusImage = "ubuntu:24.04"
	}
	if len(c.RunnerTags) == 0 {
		c.RunnerTags = []string{"self-hosted", "incus"}
	}
	return nil
}
