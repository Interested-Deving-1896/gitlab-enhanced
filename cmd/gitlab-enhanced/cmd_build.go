package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/build"
)

func newBuildCmd(cfgRoot *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build container images via the configured build backend",
		Long: `Build container images using the configured build backend.

Backends (set via build.backend in config):
  incus   — BuildKit inside a persistent Incus VM (default, local)
  depot   — Depot cloud build service (requires cloud.enabled=true)`,
	}
	cmd.AddCommand(
		newBuildStatusCmd(cfgRoot),
		newBuildImageCmd(cfgRoot),
	)
	return cmd
}

func newBuildStatusCmd(cfgRoot *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check whether the build backend is reachable",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBuildStatus(*cfgRoot)
		},
	}
}

func runBuildStatus(root string) error {
	cfg, err := loadConfig(root)
	if err != nil {
		return err
	}
	b, err := build.FromConfig(cfg)
	if err != nil {
		return fmt.Errorf("initialising build backend: %w", err)
	}

	printSection("Build backend")
	printInfo(fmt.Sprintf("backend: %s", cfg.Build.Backend))
	printInfo(fmt.Sprintf("name:    %s", b.Name()))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if b.Available(ctx) {
		printOK("build backend is reachable")
	} else {
		printFail("build backend is not reachable")
		return fmt.Errorf("build backend %q is not available", b.Name())
	}
	return nil
}

func newBuildImageCmd(cfgRoot *string) *cobra.Command {
	var (
		dockerfile string
		tags       []string
		buildArgs  []string
		platforms  []string
		push       bool
		cacheFrom  []string
	)

	cmd := &cobra.Command{
		Use:   "image <context-dir>",
		Short: "Build a container image from a Dockerfile",
		Long: `Builds a container image from the given build context directory.
Output is streamed to stdout. On success, prints the image digest and tags.

Examples:
  # Build and tag locally
  gitlab-enhanced build image . --tag myapp:latest

  # Multi-platform build and push
  gitlab-enhanced build image . \
    --tag registry.gitlab.com/mygroup/myapp:latest \
    --platform linux/amd64 --platform linux/arm64 \
    --push`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBuildImage(*cfgRoot, args[0], dockerfile, tags, buildArgs, platforms, push, cacheFrom)
		},
	}

	cmd.Flags().StringVar(&dockerfile, "dockerfile", "Dockerfile", "path to Dockerfile, relative to context dir")
	cmd.Flags().StringArrayVar(&tags, "tag", nil, "image tag (e.g. myapp:latest), repeatable")
	cmd.Flags().StringArrayVar(&buildArgs, "build-arg", nil, "build argument KEY=VALUE, repeatable")
	cmd.Flags().StringArrayVar(&platforms, "platform", nil, "target platform (e.g. linux/amd64), repeatable")
	cmd.Flags().BoolVar(&push, "push", false, "push image to registry after build")
	cmd.Flags().StringArrayVar(&cacheFrom, "cache-from", nil, "image reference to use as cache source, repeatable")

	return cmd
}

func runBuildImage(root, contextDir, dockerfile string, tags, buildArgs, platforms []string, push bool, cacheFrom []string) error {
	cfg, err := loadConfig(root)
	if err != nil {
		return err
	}
	b, err := build.FromConfig(cfg)
	if err != nil {
		return fmt.Errorf("initialising build backend: %w", err)
	}

	// Parse KEY=VALUE build args.
	argMap := make(map[string]string, len(buildArgs))
	for _, kv := range buildArgs {
		k, v, _ := splitKV(kv)
		if k != "" {
			argMap[k] = v
		}
	}

	req := build.BuildRequest{
		ContextDir: contextDir,
		Dockerfile: dockerfile,
		Tags:       tags,
		BuildArgs:  argMap,
		Platforms:  platforms,
		Push:       push,
		CacheFrom:  cacheFrom,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	printSection("Build")
	printInfo(fmt.Sprintf("backend:    %s", b.Name()))
	printInfo(fmt.Sprintf("context:    %s", contextDir))
	printInfo(fmt.Sprintf("dockerfile: %s", dockerfile))
	if len(tags) > 0 {
		printInfo(fmt.Sprintf("tags:       %v", tags))
	}
	if len(platforms) > 0 {
		printInfo(fmt.Sprintf("platforms:  %v", platforms))
	}
	fmt.Println()

	result, err := b.Build(ctx, req, os.Stdout)
	if err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	fmt.Println()
	printSection("Build result")
	printOK(fmt.Sprintf("image ID:  %s", result.ImageID))
	printInfo(fmt.Sprintf("duration:  %s", result.BuildDuration))
	for _, tag := range result.Tags {
		printOK(fmt.Sprintf("tagged:    %s", tag))
	}
	return nil
}

// splitKV splits "KEY=VALUE" into ("KEY", "VALUE"). If no "=" is present,
// returns (s, "").
func splitKV(s string) (string, string, bool) {
	for i, c := range s {
		if c == '=' {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}
