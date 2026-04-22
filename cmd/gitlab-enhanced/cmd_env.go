package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/environment"
)

func newEnvCmd(cfgRoot *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage dev environments",
		Long:  `Create, list, stop, start, and delete dev environments.`,
	}

	cmd.AddCommand(
		newEnvCreateCmd(cfgRoot),
		newEnvListCmd(cfgRoot),
		newEnvStopCmd(cfgRoot),
		newEnvStartCmd(cfgRoot),
		newEnvDeleteCmd(cfgRoot),
	)

	return cmd
}

func newEnvCreateCmd(cfgRoot *string) *cobra.Command {
	var (
		branch    string
		image     string
		ide       string
		cpus      int
		memory    string
		disk      string
		noWait    bool
	)

	cmd := &cobra.Command{
		Use:   "create <repo-url>",
		Short: "Create a new dev environment for a repository",
		Args:  cobra.ExactArgs(1),
		Example: `  # Create an environment for a GitLab repo
  gitlab-enhanced env create https://gitlab.com/mygroup/myrepo

  # Specific branch with more resources
  gitlab-enhanced env create https://gitlab.com/mygroup/myrepo \
    --branch feature/my-feature --cpus 4 --memory 8GiB`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEnvCreate(*cfgRoot, args[0], branch, image, ide, cpus, memory, disk, noWait)
		},
	}

	cmd.Flags().StringVar(&branch, "branch", "", "git branch or ref to check out")
	cmd.Flags().StringVar(&image, "image", "", "workspace image (overrides config default)")
	cmd.Flags().StringVar(&ide, "ide", "openvscode-server", "IDE to start (openvscode-server|supervisor)")
	cmd.Flags().IntVar(&cpus, "cpus", 0, "number of vCPUs (0 = use profile default)")
	cmd.Flags().StringVar(&memory, "memory", "", "memory limit e.g. 4GiB (empty = profile default)")
	cmd.Flags().StringVar(&disk, "disk", "", "disk size e.g. 30GiB (empty = profile default)")
	cmd.Flags().BoolVar(&noWait, "no-wait", false, "return immediately without waiting for IDE to be ready")

	return cmd
}

func runEnvCreate(root, repoURL, branch, image, ide string, cpus int, memory, disk string, noWait bool) error {
	cfg, err := loadConfig(root)
	if err != nil {
		return err
	}

	mgr, err := environment.FromConfig(cfg)
	if err != nil {
		return fmt.Errorf("initialising environment backend: %w", err)
	}

	if !mgr.Available(context.Background()) {
		return fmt.Errorf("environment backend %q is not available — check Incus is running", mgr.Name())
	}

	id := envIDFromURL(repoURL, branch)
	spec := environment.Spec{
		ID:      id,
		RepoURL: repoURL,
		Branch:  branch,
		Image:   image,
		IDE:     ide,
		Resources: environment.ResourceSpec{
			CPUs:   cpus,
			Memory: memory,
			Disk:   disk,
		},
	}
	if !noWait {
		spec.Timeout = 30 * time.Minute
	}

	printSection("Creating environment")
	printInfo(fmt.Sprintf("repo:    %s", repoURL))
	if branch != "" {
		printInfo(fmt.Sprintf("branch:  %s", branch))
	}
	printInfo(fmt.Sprintf("backend: %s", mgr.Name()))
	printInfo(fmt.Sprintf("ide:     %s", ide))
	fmt.Println()

	ctx := context.Background()
	env, err := mgr.Create(ctx, spec)
	if err != nil {
		return fmt.Errorf("creating environment: %w", err)
	}

	printOK(fmt.Sprintf("environment %q created", env.ID))
	if env.IDEURL != "" {
		fmt.Printf("\n  \033[1mOpen in browser:\033[0m  %s\n", env.IDEURL)
	}
	if env.SSHHost != "" {
		fmt.Printf("  \033[1mSSH access:\033[0m       %s\n", env.SSHHost)
	}
	return nil
}

func newEnvListCmd(cfgRoot *string) *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List dev environments",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEnvList(*cfgRoot, all)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include stopped environments")
	return cmd
}

func runEnvList(root string, all bool) error {
	cfg, err := loadConfig(root)
	if err != nil {
		return err
	}
	mgr, err := environment.FromConfig(cfg)
	if err != nil {
		return err
	}

	var statusFilter environment.Status
	if !all {
		statusFilter = environment.StatusRunning
	}

	envs, err := mgr.List(context.Background(), statusFilter)
	if err != nil {
		return fmt.Errorf("listing environments: %w", err)
	}

	if len(envs) == 0 {
		if all {
			fmt.Println("No environments found.")
		} else {
			fmt.Println("No running environments. Use --all to show stopped environments.")
		}
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSTATUS\tREPO\tBRANCH\tIDE URL")
	fmt.Fprintln(w, "──\t──────\t────\t──────\t───────")
	for _, env := range envs {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			env.ID,
			colorStatus(env.Status),
			env.Spec.RepoURL,
			env.Spec.Branch,
			env.IDEURL,
		)
	}
	return w.Flush()
}

func newEnvStopCmd(cfgRoot *string) *cobra.Command {
	return &cobra.Command{
		Use:   "stop <id>",
		Short: "Stop a running environment (preserves data)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEnvAction(*cfgRoot, args[0], "stop")
		},
	}
}

func newEnvStartCmd(cfgRoot *string) *cobra.Command {
	return &cobra.Command{
		Use:   "start <id>",
		Short: "Start a stopped environment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEnvAction(*cfgRoot, args[0], "start")
		},
	}
}

func newEnvDeleteCmd(cfgRoot *string) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete an environment and free all resources",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !force {
				fmt.Printf("Delete environment %q? This cannot be undone. [y/N] ", args[0])
				var confirm string
				fmt.Scanln(&confirm)
				if confirm != "y" && confirm != "Y" {
					fmt.Println("Aborted.")
					return nil
				}
			}
			return runEnvAction(*cfgRoot, args[0], "delete")
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "skip confirmation prompt")
	return cmd
}

func runEnvAction(root, id, action string) error {
	cfg, err := loadConfig(root)
	if err != nil {
		return err
	}
	mgr, err := environment.FromConfig(cfg)
	if err != nil {
		return err
	}

	ctx := context.Background()
	switch action {
	case "stop":
		err = mgr.Stop(ctx, id)
	case "start":
		err = mgr.Start(ctx, id)
	case "delete":
		err = mgr.Delete(ctx, id)
	}
	if err != nil {
		return fmt.Errorf("%s environment %q: %w", action, id, err)
	}
	printOK(fmt.Sprintf("environment %q %sed", id, action))
	return nil
}

// envIDFromURL derives a short stable ID from a repo URL and branch.
func envIDFromURL(repoURL, branch string) string {
	// Extract last path segment of URL
	parts := splitPath(repoURL)
	name := parts[len(parts)-1]
	// Strip .git suffix
	if len(name) > 4 && name[len(name)-4:] == ".git" {
		name = name[:len(name)-4]
	}
	if branch != "" {
		// Use first 8 chars of branch, sanitised
		b := sanitiseID(branch)
		if len(b) > 8 {
			b = b[:8]
		}
		return sanitiseID(name) + "-" + b
	}
	return sanitiseID(name) + "-" + shortTimestamp()
}

func sanitiseID(s string) string {
	var b []byte
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			b = append(b, c)
		} else if c >= 'A' && c <= 'Z' {
			b = append(b, c+32) // toLower
		} else {
			b = append(b, '-')
		}
	}
	return string(b)
}

func shortTimestamp() string {
	return fmt.Sprintf("%x", time.Now().Unix())[4:]
}

func splitPath(url string) []string {
	// Simple split on / — handles both http and ssh URLs
	parts := []string{}
	current := ""
	for _, c := range url {
		if c == '/' || c == ':' {
			if current != "" {
				parts = append(parts, current)
			}
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	if len(parts) == 0 {
		return []string{"env"}
	}
	return parts
}

func colorStatus(s environment.Status) string {
	switch s {
	case environment.StatusRunning:
		return "\033[32mrunning\033[0m"
	case environment.StatusStarting:
		return "\033[33mstarting\033[0m"
	case environment.StatusStopped:
		return "\033[90mstopped\033[0m"
	case environment.StatusError:
		return "\033[31merror\033[0m"
	default:
		return string(s)
	}
}
