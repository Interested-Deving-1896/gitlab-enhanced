package environment

import (
	"fmt"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/config"
)

// FromConfig returns the appropriate Manager from configuration.
func FromConfig(cfg *config.Config) (Manager, error) {
	switch cfg.Environment.Backend {
	case "incus", "":
		return NewIncusManager(
			cfg.Build.Socket,
			"workspace-default",
			cfg.Environment.Network,
			cfg.Environment.IDEPort,
		), nil

	case "gitpod-k8s":
		// Gitpod Classic running on K8s-in-Incus.
		// The K8s cluster is provisioned by runtime/k8s-in-incus/ansible/.
		return NewGitpodK8sManager(cfg), nil

	case "ona":
		if !cfg.Cloud.Enabled {
			return nil, fmt.Errorf("environment.backend is 'ona' but cloud.enabled is false")
		}
		return NewOnaManager(cfg.Environment.Token), nil

	default:
		return nil, fmt.Errorf("unknown environment backend: %q", cfg.Environment.Backend)
	}
}
