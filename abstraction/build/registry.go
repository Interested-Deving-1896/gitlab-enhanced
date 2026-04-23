package build

import (
	"fmt"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/config"
)

// FromConfig returns the appropriate Builder from configuration.
func FromConfig(cfg *config.Config) (Builder, error) {
	switch cfg.Build.Backend {
	case "incus", "":
		return NewIncusBuilder(cfg.Build.Socket, cfg.Build.CachePool), nil

	case "depot":
		if !cfg.Cloud.Enabled {
			return nil, fmt.Errorf("build.backend is 'depot' but cloud.enabled is false")
		}
		return NewDepotBuilder(cfg.Build.ProjectID, cfg.Build.Token), nil

	default:
		return nil, fmt.Errorf("unknown build backend: %q", cfg.Build.Backend)
	}
}
