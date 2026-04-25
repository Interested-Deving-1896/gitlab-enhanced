package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/runner"
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
