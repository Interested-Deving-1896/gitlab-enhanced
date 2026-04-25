package main

import (
	"os"

	"github.com/spf13/cobra"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/version"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var cfgRoot string

	root := &cobra.Command{
		Use:     "gitlab-enhanced",
		Short:   "Local-first GitLab platform — packaging, environments, LFS, CI",
		Version: version.String(),
		Long: `gitlab-enhanced manages a self-hosted GitLab stack running on Incus.

All components run locally by default. Cloud providers are opt-in overlays.

Configuration is loaded from (later overrides earlier):
  <root>/config/defaults.yaml   committed defaults
  <root>/config/local.yaml      machine-specific overrides (gitignored)
  <root>/config/cloud.yaml      cloud overlay (applied only when cloud.enabled=true)
  GITLAB_ENHANCED_* env vars    highest priority`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVar(&cfgRoot, "config-root", repoRoot(),
		"path to the gitlab-enhanced repository root (contains config/)")

	root.AddCommand(
		newInitCmd(&cfgRoot),
		newUpCmd(&cfgRoot),
		newDownCmd(&cfgRoot),
		newEnvCmd(&cfgRoot),
		newLFSCmd(&cfgRoot),
		newStatusCmd(&cfgRoot),
		newRewardsCmd(&cfgRoot),
		newBandwidthCmd(&cfgRoot),
		newRunnerCmd(&cfgRoot),
		newBuildCmd(&cfgRoot),
		newStorageCmd(&cfgRoot),
	)

	return root
}
