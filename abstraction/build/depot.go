package build

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// DepotBuilder delegates builds to Depot's remote cloud builders via the depot CLI.
// Used only when cloud.enabled=true. The depot CLI must be installed in PATH.
//
// Depot provides 16 CPUs, 32GB RAM, persistent 50GB cache, native multi-arch.
// See: https://depot.dev
type DepotBuilder struct {
	projectID string
	token     string
}

func NewDepotBuilder(projectID, token string) *DepotBuilder {
	return &DepotBuilder{projectID: projectID, token: token}
}

func (b *DepotBuilder) Name() string { return "depot:" + b.projectID }

// Available checks that the depot CLI is installed and the project is reachable.
func (b *DepotBuilder) Available(ctx context.Context) bool {
	if b.projectID == "" || b.token == "" {
		return false
	}
	// Check depot CLI is in PATH
	if _, err := exec.LookPath("depot"); err != nil {
		return false
	}
	// Ping the project via `depot projects list`
	cmd := exec.CommandContext(ctx, "depot", "projects", "list", "--output", "json")
	cmd.Env = append(cmd.Environ(), "DEPOT_TOKEN="+b.token)
	return cmd.Run() == nil
}

// Build runs `depot build` as a subprocess, streaming output to logs.
func (b *DepotBuilder) Build(ctx context.Context, req BuildRequest, logs io.Writer) (*BuildResult, error) {
	start := time.Now()

	depotPath, err := exec.LookPath("depot")
	if err != nil {
		return nil, fmt.Errorf("depot CLI not found in PATH: install from https://depot.dev/docs/cli/installation")
	}

	args := []string{
		"build",
		"--project", b.projectID,
		"--progress", "plain",
	}

	// Dockerfile
	if req.Dockerfile != "" {
		args = append(args, "--file", req.Dockerfile)
	}

	// Tags
	for _, tag := range req.Tags {
		args = append(args, "--tag", tag)
	}

	// Push
	if req.Push {
		args = append(args, "--push")
	} else {
		args = append(args, "--load")
	}

	// Build args
	for k, v := range req.BuildArgs {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", k, v))
	}

	// Platforms
	if len(req.Platforms) > 0 {
		args = append(args, "--platform", strings.Join(req.Platforms, ","))
	}

	// Cache sources
	for _, c := range req.CacheFrom {
		args = append(args, "--cache-from", c)
	}

	// Build context (must be last)
	contextDir := req.ContextDir
	if contextDir == "" {
		contextDir = "."
	}
	args = append(args, contextDir)

	cmd := exec.CommandContext(ctx, depotPath, args...)
	cmd.Env = append(cmd.Environ(), "DEPOT_TOKEN="+b.token)

	var stderr bytes.Buffer
	cmd.Stdout = logs
	cmd.Stderr = io.MultiWriter(logs, &stderr)

	fmt.Fprintf(logs, "[depot] running: depot %s\n", strings.Join(args, " "))
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("depot build failed: %w\nstderr: %s", err, stderr.String())
	}

	return &BuildResult{
		Tags:          req.Tags,
		BuildDuration: time.Since(start).Round(time.Second).String(),
	}, nil
}

var _ Builder = (*DepotBuilder)(nil)
