package main

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newTemplateCmd(cfgRoot *string) *cobra.Command {
	template := &cobra.Command{
		Use:   "template",
		Short: "Manage template file propagation to GitLab consumers",
		Long: `Commands for propagating shared template files from fork-sync-all-gitlab
to registered GitLab consumer projects.

Uses fork-sync-all-gitlab scripts under the hood (Stage A integration).`,
	}

	template.AddCommand(
		newTemplateSyncCmd(cfgRoot),
	)
	return template
}

func newTemplateSyncCmd(cfgRoot *string) *cobra.Command {
	var dryRun bool
	var force bool
	var repoFilter string

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Propagate template files to registered GitLab consumers",
		Long: `Reads config/template-consumers-gitlab.yml and pushes template files
to each registered project via the GitLab Contents API.

Required env: GITLAB_TOKEN
Optional env: GITLAB_URL`,
		RunE: func(cmd *cobra.Command, args []string) error {
			script := resolveTemplateScript(*cfgRoot)
			if script == "" {
				return errScriptNotFound("sync-template-gitlab.sh")
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
			return runScript(script, env)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Report what would be synced without making changes")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite files that already exist in consumers")
	cmd.Flags().StringVar(&repoFilter, "filter", "", "Only sync to consumers whose name contains this substring")
	return cmd
}

func resolveTemplateScript(cfgRoot string) string {
	if dir := os.Getenv("FORK_SYNC_GITLAB_DIR"); dir != "" {
		p := filepath.Join(dir, "scripts", "sync-template-gitlab.sh")
		if fileExists(p) {
			return p
		}
	}
	candidates := []string{
		filepath.Join(cfgRoot, "..", "fork-sync-all-gitlab", "scripts", "sync-template-gitlab.sh"),
		filepath.Join(cfgRoot, "fork-sync-all-gitlab", "scripts", "sync-template-gitlab.sh"),
	}
	for _, p := range candidates {
		if fileExists(p) {
			return p
		}
	}
	return ""
}
