package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/runner"
)

const (
	executorInstallDir = "/usr/local/lib/gitlab-runner-incus"
	defaultGitLabURL   = "https://gitlab.com"
	defaultTagList     = "incus,self-hosted"
	defaultDescription = "gitlab-enhanced Incus runner"
)

func newRunnerCmd(cfgRoot *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runner",
		Short: "Manage the CI runner backend",
		Long: `Inspect and interact with the configured CI runner backend.

Backends (set via runner.backend in config):
  incus       — ephemeral Incus VMs (default, local)
  blacksmith  — Blacksmith Firecracker runners (cloud, requires cloud.enabled=true)`,
	}
	cmd.AddCommand(
		newRunnerStatusCmd(cfgRoot),
		newRunnerRunCmd(cfgRoot),
		newRunnerRegisterCmd(cfgRoot),
	)
	return cmd
}

func newRunnerStatusCmd(cfgRoot *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check whether the runner backend is reachable",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunnerStatus(*cfgRoot)
		},
	}
}

func runRunnerStatus(root string) error {
	cfg, err := loadConfig(root)
	if err != nil {
		return err
	}
	r, err := runner.FromConfig(cfg)
	if err != nil {
		return fmt.Errorf("initialising runner backend: %w", err)
	}

	printSection("Runner backend")
	printInfo(fmt.Sprintf("backend: %s", cfg.Runner.Backend))
	printInfo(fmt.Sprintf("name:    %s", r.Name()))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if r.Available(ctx) {
		printOK("runner backend is reachable")
	} else {
		printFail("runner backend is not reachable")
		return fmt.Errorf("runner backend %q is not available", r.Name())
	}
	return nil
}

func newRunnerRunCmd(cfgRoot *string) *cobra.Command {
	var (
		image     string
		env       []string
		artifacts []string
		timeout   time.Duration
		cpus      int
		memory    string
	)

	cmd := &cobra.Command{
		Use:   "run <command> [args...]",
		Short: "Execute a command as a CI job on the configured runner backend",
		Long: `Runs a single command as a CI job on the configured runner backend.
Output is streamed to stdout. The exit code of the job is propagated.

Example:
  gitlab-enhanced runner run --image ubuntu:22.04 -- go test ./...
  gitlab-enhanced runner run --image golang:1.25 --env GOPROXY=direct -- go build ./...`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunnerRun(*cfgRoot, args, image, env, artifacts, timeout, cpus, memory)
		},
	}

	cmd.Flags().StringVar(&image, "image", "", "container/VM image to run the job in")
	cmd.Flags().StringArrayVar(&env, "env", nil, "environment variables (KEY=VALUE), repeatable")
	cmd.Flags().StringArrayVar(&artifacts, "artifact", nil, "paths to collect as artifacts after the job, repeatable")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "maximum job duration")
	cmd.Flags().IntVar(&cpus, "cpus", 0, "number of vCPUs (0 = backend default)")
	cmd.Flags().StringVar(&memory, "memory", "", "memory limit e.g. 4GiB (empty = backend default)")

	return cmd
}

func runRunnerRun(root string, commands []string, image string, envVars []string, artifacts []string, timeout time.Duration, cpus int, memory string) error {
	cfg, err := loadConfig(root)
	if err != nil {
		return err
	}
	r, err := runner.FromConfig(cfg)
	if err != nil {
		return fmt.Errorf("initialising runner backend: %w", err)
	}

	// Parse KEY=VALUE env pairs.
	envMap := make(map[string]string, len(envVars))
	for _, kv := range envVars {
		k, v, _ := strings.Cut(kv, "=")
		if k != "" {
			envMap[k] = v
		}
	}

	job := runner.JobSpec{
		ID:        fmt.Sprintf("cli-%d", time.Now().UnixMilli()),
		Image:     image,
		Commands:  commands,
		Env:       envMap,
		Artifacts: artifacts,
		Timeout:   timeout,
		Resources: runner.ResourceSpec{
			CPUs:   cpus,
			Memory: memory,
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	printSection("Runner job")
	printInfo(fmt.Sprintf("backend:  %s", r.Name()))
	printInfo(fmt.Sprintf("image:    %s", image))
	printInfo(fmt.Sprintf("command:  %s", strings.Join(commands, " ")))
	printInfo(fmt.Sprintf("timeout:  %s", timeout))
	fmt.Println()

	result, err := r.Run(ctx, job, io.MultiWriter(os.Stdout, os.Stderr))
	if err != nil {
		return fmt.Errorf("job failed: %w", err)
	}

	fmt.Println()
	printSection("Job result")
	printInfo(fmt.Sprintf("exit code: %d", result.ExitCode))
	printInfo(fmt.Sprintf("duration:  %s", result.Duration.Round(time.Millisecond)))
	for name, path := range result.ArtifactPaths {
		printOK(fmt.Sprintf("artifact %q → %s", name, path))
	}

	if result.ExitCode != 0 {
		return fmt.Errorf("job exited with code %d", result.ExitCode)
	}
	return nil
}

func newRunnerRegisterCmd(cfgRoot *string) *cobra.Command {
	var (
		token       string
		gitlabURL   string
		description string
		tags        string
		install     bool
	)

	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register this machine as a self-hosted Incus CI runner",
		Long: `Registers this machine as a GitLab CI runner using the Incus custom executor.

What this command does:
  1. Verifies that gitlab-runner and incus are installed
  2. Optionally installs the executor scripts to ` + executorInstallDir + ` (--install)
  3. Calls 'gitlab-runner register' with the correct custom executor flags

The runner will pick up jobs tagged [incus, self-hosted] from your GitLab project.

Obtain a runner token from:
  GitLab → Project → Settings → CI/CD → Runners → New project runner
  (set tags: incus, self-hosted)

Example:
  sudo gitlab-enhanced runner register --token glrt-xxxxxxxxxxxxxxxxxxxx
  sudo gitlab-enhanced runner register --token glrt-xxx --url https://gitlab.example.com`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunnerRegister(*cfgRoot, token, gitlabURL, description, tags, install)
		},
	}

	cmd.Flags().StringVar(&token, "token", "", "runner authentication token from GitLab (required)")
	cmd.Flags().StringVar(&gitlabURL, "url", defaultGitLabURL, "GitLab instance URL")
	cmd.Flags().StringVar(&description, "description", defaultDescription, "runner description shown in GitLab UI")
	cmd.Flags().StringVar(&tags, "tags", defaultTagList, "comma-separated list of runner tags")
	cmd.Flags().BoolVar(&install, "install", true, "install executor scripts to "+executorInstallDir+" before registering")
	_ = cmd.MarkFlagRequired("token")

	return cmd
}

func runRunnerRegister(root, token, gitlabURL, description, tags string, install bool) error {
	// 1. Preflight checks
	printSection("Preflight checks")

	if err := checkBinary("gitlab-runner"); err != nil {
		printFail("gitlab-runner not found in PATH")
		fmt.Println()
		fmt.Println("  Install it from: https://docs.gitlab.com/runner/install/linux-repository.html")
		fmt.Println("  Quick install (Debian/Ubuntu):")
		fmt.Println("    curl -L https://packages.gitlab.com/install/repositories/runner/gitlab-runner/script.deb.sh | sudo bash")
		fmt.Println("    sudo apt-get install gitlab-runner")
		return fmt.Errorf("gitlab-runner is required")
	}
	printOK("gitlab-runner found")

	if err := checkBinary("incus"); err != nil {
		printFail("incus not found in PATH")
		fmt.Println()
		fmt.Println("  Install it from: https://linuxcontainers.org/incus/docs/main/installing/")
		return fmt.Errorf("incus is required")
	}
	printOK("incus found")

	// 2. Install executor scripts if requested
	if install {
		fmt.Println()
		printSection("Installing executor scripts")

		// Locate the scripts relative to the repo root.
		scriptSrc := filepath.Join(root, "runtime", "incus", "runner")
		if _, err := os.Stat(scriptSrc); err != nil {
			return fmt.Errorf("executor scripts not found at %s — run from the gitlab-enhanced repo root", scriptSrc)
		}

		if err := os.MkdirAll(executorInstallDir, 0755); err != nil {
			return fmt.Errorf("creating %s: %w (try running with sudo)", executorInstallDir, err)
		}

		for _, script := range []string{"config.sh", "prepare.sh", "run.sh", "cleanup.sh"} {
			src := filepath.Join(scriptSrc, script)
			dst := filepath.Join(executorInstallDir, script)
			if err := copyExecutable(src, dst); err != nil {
				return fmt.Errorf("installing %s: %w", script, err)
			}
			printOK(fmt.Sprintf("installed %s → %s", script, dst))
		}
	} else {
		// Verify scripts are already installed.
		fmt.Println()
		printSection("Checking executor scripts")
		for _, script := range []string{"config.sh", "prepare.sh", "run.sh", "cleanup.sh"} {
			p := filepath.Join(executorInstallDir, script)
			if _, err := os.Stat(p); err != nil {
				printFail(fmt.Sprintf("missing: %s", p))
				return fmt.Errorf("executor scripts not installed — re-run with --install or run: sudo bash runtime/incus/runner/install.sh")
			}
			printOK(fmt.Sprintf("found %s", p))
		}
	}

	// 3. Register the runner
	fmt.Println()
	printSection("Registering runner")
	printInfo(fmt.Sprintf("url:         %s", gitlabURL))
	printInfo(fmt.Sprintf("description: %s", description))
	printInfo(fmt.Sprintf("tags:        %s", tags))
	printInfo(fmt.Sprintf("executor:    custom (Incus)"))
	fmt.Println()

	registerArgs := []string{
		"register",
		"--non-interactive",
		"--url", gitlabURL,
		"--token", token,
		"--executor", "custom",
		"--description", description,
		"--tag-list", tags,
		"--custom-config-exec", filepath.Join(executorInstallDir, "config.sh"),
		"--custom-prepare-exec", filepath.Join(executorInstallDir, "prepare.sh"),
		"--custom-run-exec", filepath.Join(executorInstallDir, "run.sh"),
		"--custom-cleanup-exec", filepath.Join(executorInstallDir, "cleanup.sh"),
	}

	if err := runCmd("gitlab-runner", registerArgs...); err != nil {
		return fmt.Errorf("gitlab-runner register failed: %w", err)
	}

	fmt.Println()
	printOK("Runner registered successfully")
	fmt.Println()
	printInfo("Start the runner:  sudo gitlab-runner start")
	printInfo("Check status:      sudo gitlab-runner status")
	printInfo("View logs:         sudo journalctl -u gitlab-runner -f")
	fmt.Println()
	printInfo("Optional: pre-bake the CI image for faster job starts:")
	printInfo("  bash runtime/incus/images/build-ci-base.sh")

	return nil
}

// copyExecutable copies src to dst with mode 0755.
func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
