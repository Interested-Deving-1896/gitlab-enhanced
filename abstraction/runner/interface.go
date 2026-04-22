// Package runner defines the backend-agnostic CI runner interface.
// Implementations: Incus VM runner (default/local), Blacksmith (cloud secondary).
package runner

import (
	"context"
	"io"
	"time"
)

// JobSpec describes a CI job to execute.
type JobSpec struct {
	// ID is the unique job identifier from the CI system.
	ID string

	// Image is the container/VM image to run the job in.
	Image string

	// Commands are the shell commands to execute in order.
	Commands []string

	// Env is the set of environment variables for the job.
	Env map[string]string

	// Artifacts lists paths to collect after the job completes.
	Artifacts []string

	// Timeout is the maximum allowed job duration.
	Timeout time.Duration

	// Resources describes the resource requirements.
	Resources ResourceSpec
}

// ResourceSpec describes compute resource requirements.
type ResourceSpec struct {
	CPUs   int    // number of vCPUs
	Memory string // e.g. "4GiB"
	Disk   string // e.g. "20GiB"
}

// JobResult is returned when a job completes.
type JobResult struct {
	JobID    string
	ExitCode int
	Duration time.Duration
	// ArtifactPaths maps artifact name to local path where it was collected.
	ArtifactPaths map[string]string
}

// Runner is the CI runner backend interface.
type Runner interface {
	// Available reports whether this runner backend is reachable.
	Available(ctx context.Context) bool

	// Run executes a CI job, streaming output to logs.
	Run(ctx context.Context, job JobSpec, logs io.Writer) (*JobResult, error)

	// Cancel terminates a running job by ID.
	Cancel(ctx context.Context, jobID string) error

	// Name returns a human-readable identifier.
	Name() string
}
