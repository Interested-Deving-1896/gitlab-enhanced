package main

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newMirrorCmd(cfgRoot *string) *cobra.Command {
	mirror := &cobra.Command{
		Use:   "mirror",
		Short: "Manage the GitLab mirror chain",
		Long: `Commands for managing the GitLab mirror chain — pushing projects from a
source namespace to downstream namespaces and checking mirror status.

Uses fork-sync-all-gitlab scripts under the hood (Stage A integration).`,
	}

	mirror.AddCommand(
		newMirrorStatusCmd(cfgRoot),
		newMirrorPushCmd(cfgRoot),
	)
	return mirror
}

func newMirrorStatusCmd(cfgRoot *string) *cobra.Command {
	var repoFilter string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show mirror chain status (dry run)",
		Long: `Reports which projects would be pushed to downstream namespaces without
making any changes.

Required env: GITLAB_TOKEN, GITLAB_SOURCE_NS
Optional env: GITLAB_URL, GITLAB_DOWNSTREAM`,
		RunE: func(cmd *cobra.Command, args []string) error {
			script := resolveMirrorScript(*cfgRoot)
			if script == "" {
				return errScriptNotFound("mirror-chain-gitlab.sh")
			}
			env := append(os.Environ(), "DRY_RUN=true")
			if repoFilter != "" {
				env = append(env, "REPO_FILTER="+repoFilter)
			}
			return runScript(script, env)
		},
	}

	cmd.Flags().StringVar(&repoFilter, "filter", "", "Only show repos whose name contains this substring")
	return cmd
}

func newMirrorPushCmd(cfgRoot *string) *cobra.Command {
	var dryRun bool
	var repoFilter string

	cmd := &cobra.Command{
		Use:   "push",
		Short: "Push mirror chain to downstream namespaces",
		Long: `Pushes all projects from GITLAB_SOURCE_NS to each namespace in
GITLAB_DOWNSTREAM, creating projects if they don't exist.

Required env: GITLAB_TOKEN, GITLAB_SOURCE_NS, GITLAB_DOWNSTREAM
Optional env: GITLAB_URL`,
		RunE: func(cmd *cobra.Command, args []string) error {
			script := resolveMirrorScript(*cfgRoot)
			if script == "" {
				return errScriptNotFound("mirror-chain-gitlab.sh")
			}
			env := os.Environ()
			if dryRun {
				env = append(env, "DRY_RUN=true")
			}
			if repoFilter != "" {
				env = append(env, "REPO_FILTER="+repoFilter)
			}
			return runScript(script, env)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Report what would be pushed without making changes")
	cmd.Flags().StringVar(&repoFilter, "filter", "", "Only push repos whose name contains this substring")
	return cmd
}

func resolveMirrorScript(cfgRoot string) string {
	if dir := os.Getenv("FORK_SYNC_GITLAB_DIR"); dir != "" {
		p := filepath.Join(dir, "scripts", "mirror-chain-gitlab.sh")
		if fileExists(p) {
			return p
		}
	}
	candidates := []string{
		filepath.Join(cfgRoot, "..", "fork-sync-all-gitlab", "scripts", "mirror-chain-gitlab.sh"),
		filepath.Join(cfgRoot, "fork-sync-all-gitlab", "scripts", "mirror-chain-gitlab.sh"),
	}
	for _, p := range candidates {
		if fileExists(p) {
			return p
		}
	}
	return ""
}
