package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newForkCmd(cfgRoot *string) *cobra.Command {
	fork := &cobra.Command{
		Use:   "fork",
		Short: "Manage GitLab fork synchronisation",
		Long: `Commands for syncing GitLab forks with their upstream sources.

Uses fork-sync-all-gitlab scripts under the hood (Stage A integration).
Scripts are resolved from FORK_SYNC_GITLAB_DIR or the fork-sync-all-gitlab
subdirectory relative to the config root.`,
	}

	fork.AddCommand(
		newForkSyncCmd(cfgRoot),
	)
	return fork
}

func newForkSyncCmd(cfgRoot *string) *cobra.Command {
	var dryRun bool
	var force bool
	var repoFilter string
	var branchFilter string

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync all GitLab forks to their upstream sources",
		Long: `Iterates all GitLab forks owned by GITLAB_OWNER and syncs each to its
upstream via the GitLab merge_upstream API. Falls back to direct git
operations when the API returns a conflict or error.

Required env: GITLAB_TOKEN, GITLAB_OWNER
Optional env: GITLAB_URL (default: https://gitlab.com)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			script := resolveForkSyncScript(*cfgRoot)
			if script == "" {
				return fmt.Errorf("sync-all-forks-gitlab.sh not found — set FORK_SYNC_GITLAB_DIR or place fork-sync-all-gitlab adjacent to config root")
			}

			env := os.Environ()
			if dryRun {
				env = append(env, "DRY_RUN=true")
			}
			if force {
				env = append(env, "FORCE=true")
			}
			if repoFilter != "" {
				env = append(env, "REPO_FILTER="+repoFilter)
			}
			if branchFilter != "" {
				env = append(env, "BRANCH_FILTER="+branchFilter)
			}

			return runScript(script, env)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Report what would be synced without making changes")
	cmd.Flags().BoolVar(&force, "force", false, "Reset diverged branches to upstream HEAD")
	cmd.Flags().StringVar(&repoFilter, "filter", "", "Only sync repos whose name contains this substring")
	cmd.Flags().StringVar(&branchFilter, "branch", "", "Only sync repos whose default branch matches")

	return cmd
}

// ── helpers ───────────────────────────────────────────────────────────────────

func resolveForkSyncScript(cfgRoot string) string {
	// 1. Explicit env override
	if dir := os.Getenv("FORK_SYNC_GITLAB_DIR"); dir != "" {
		p := filepath.Join(dir, "scripts", "sync-all-forks-gitlab.sh")
		if fileExists(p) {
			return p
		}
	}

	// 2. Adjacent to config root
	candidates := []string{
		filepath.Join(cfgRoot, "..", "fork-sync-all-gitlab", "scripts", "sync-all-forks-gitlab.sh"),
		filepath.Join(cfgRoot, "fork-sync-all-gitlab", "scripts", "sync-all-forks-gitlab.sh"),
	}
	for _, p := range candidates {
		if fileExists(p) {
			return p
		}
	}
	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func runScript(script string, env []string) error {
	cmd := exec.Command("bash", script)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
