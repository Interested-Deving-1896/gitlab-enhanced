package runner

import (
	"fmt"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/config"
)

// FromConfig returns the appropriate Runner from configuration.
func FromConfig(cfg *config.Config) (Runner, error) {
	switch cfg.Runner.Backend {
	case "incus", "":
		return NewIncusRunner(
			cfg.Build.Socket,
			cfg.Runner.VMProfile,
			cfg.Environment.Network,
		), nil

	case "blacksmith":
		if !cfg.Cloud.Enabled {
			return nil, fmt.Errorf("runner.backend is 'blacksmith' but cloud.enabled is false")
		}
		r := NewBlacksmithRunner(cfg.Runner.Org, cfg.Runner.Token)
		if cfg.Runner.BlacksmithAPIURL != "" {
			r = r.WithAPIURL(cfg.Runner.BlacksmithAPIURL)
		}
		return r, nil

	default:
		return nil, fmt.Errorf("unknown runner backend: %q", cfg.Runner.Backend)
	}
}
