package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newOTACmd(cfgRoot *string) *cobra.Command {
	ota := &cobra.Command{
		Use:   "ota",
		Short: "Manage OTA updates for opted-in GitLab projects",
		Long: `Commands for delivering and discovering OTA updates across opted-in
GitLab projects.

Uses fork-sync-all-gitlab scripts under the hood (Stage A integration).`,
	}

	ota.AddCommand(
		newOTADeliverCmd(cfgRoot),
		newOTADiscoverCmd(cfgRoot),
	)
	return ota
}

func newOTADeliverCmd(cfgRoot *string) *cobra.Command {
	var dryRun bool
	var repoFilter string
	var version string

	cmd := &cobra.Command{
		Use:   "deliver",
		Short: "Deliver OTA updates to opted-in GitLab projects",
		Long: `Opens MRs against all opted-in GitLab projects in ota-registry-gitlab.yml
with the assembled OTA payload for the given version.

Required env: GITLAB_TOKEN, OTA_VERSION (or --version flag)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			script := resolveOTAScript(*cfgRoot, "ota-deliver-gitlab.sh")
			if script == "" {
				return errScriptNotFound("ota-deliver-gitlab.sh")
			}

			if version == "" {
				version = os.Getenv("OTA_VERSION")
			}
			if version == "" {
				return fmt.Errorf("--version or OTA_VERSION env var is required")
			}

			env := append(os.Environ(), "OTA_VERSION="+version)
			if dryRun {
				env = append(env, "DRY_RUN=true")
			}
			if repoFilter != "" {
				env = append(env, "REPO_FILTER="+repoFilter)
			}
			return runScript(script, env)
		},
	}

	cmd.Flags().StringVar(&version, "version", "", "OTA version tag to deliver (e.g. v1.2.3)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Assemble payloads but do not open MRs")
	cmd.Flags().StringVar(&repoFilter, "filter", "", "Only deliver to projects whose name contains this substring")
	return cmd
}

func newOTADiscoverCmd(cfgRoot *string) *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Scan GitLab forks for new OTA opt-ins",
		Long: `Scans GitLab forks of fork-sync-all-gitlab for .ota/config.yml with
enabled: true. Adds newly discovered projects to ota-registry-gitlab.yml
and opens an MR with the changes.

Required env: GITLAB_TOKEN`,
		RunE: func(cmd *cobra.Command, args []string) error {
			script := resolveOTAScript(*cfgRoot, "ota-discover-gitlab.sh")
			if script == "" {
				return errScriptNotFound("ota-discover-gitlab.sh")
			}
			env := os.Environ()
			if dryRun {
				env = append(env, "DRY_RUN=true")
			}
			return runScript(script, env)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Report new opt-ins without updating registry")
	return cmd
}

func resolveOTAScript(cfgRoot, scriptName string) string {
	if dir := os.Getenv("FORK_SYNC_GITLAB_DIR"); dir != "" {
		p := filepath.Join(dir, "scripts", scriptName)
		if fileExists(p) {
			return p
		}
	}
	candidates := []string{
		filepath.Join(cfgRoot, "..", "fork-sync-all-gitlab", "scripts", scriptName),
		filepath.Join(cfgRoot, "fork-sync-all-gitlab", "scripts", scriptName),
	}
	for _, p := range candidates {
		if fileExists(p) {
			return p
		}
	}
	return ""
}

// errScriptNotFound is defined in cmd_fork.go (shared across fork/mirror/ota/template commands)
