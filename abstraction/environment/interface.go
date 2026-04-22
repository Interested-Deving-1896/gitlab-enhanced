// Package environment defines the backend-agnostic dev environment interface.
// Implementations: Incus (default/local), Gitpod Classic K8s, Ona cloud (secondary).
//
// An "environment" is an ephemeral, pre-configured workspace containing:
//   - a cloned git repository
//   - language toolchains and dependencies
//   - a running IDE (OpenVSCode Server by default)
//   - port forwarding to the host
package environment

import (
	"context"
	"time"
)

// Spec describes the desired environment.
type Spec struct {
	// ID is a unique identifier for this environment instance.
	ID string

	// RepoURL is the git repository to clone into the environment.
	RepoURL string

	// Branch is the git branch/ref to check out. Defaults to default branch.
	Branch string

	// Image is the workspace image to use.
	// Defaults to config.Environment.WorkspaceImage.
	Image string

	// IDE is the IDE to start. Defaults to "openvscode-server".
	IDE string

	// DevcontainerPath is the path to devcontainer.json within the repo.
	// Falls back to .gitpod.yml if not found.
	DevcontainerPath string

	// Resources describes compute requirements.
	Resources ResourceSpec

	// Env is additional environment variables to inject.
	Env map[string]string

	// Timeout is the idle timeout after which the environment is stopped.
	Timeout time.Duration
}

// ResourceSpec describes compute resource requirements for an environment.
type ResourceSpec struct {
	CPUs   int
	Memory string // e.g. "8GiB"
	Disk   string // e.g. "30GiB"
}

// Status represents the lifecycle state of an environment.
type Status string

const (
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusStopped  Status = "stopped"
	StatusError    Status = "error"
)

// Environment represents a running or stopped dev environment instance.
type Environment struct {
	ID      string
	Spec    Spec
	Status  Status
	IDEURL  string // URL to open the IDE in a browser
	SSHHost string // SSH connection string for terminal access
	Created time.Time
	Stopped time.Time
}

// Manager is the environment lifecycle backend interface.
type Manager interface {
	// Available reports whether this backend is reachable.
	Available(ctx context.Context) bool

	// Create provisions and starts a new environment.
	Create(ctx context.Context, spec Spec) (*Environment, error)

	// Get returns the current state of an environment by ID.
	Get(ctx context.Context, id string) (*Environment, error)

	// List returns all environments, optionally filtered by status.
	List(ctx context.Context, status Status) ([]*Environment, error)

	// Stop halts a running environment without destroying it.
	Stop(ctx context.Context, id string) error

	// Start resumes a stopped environment.
	Start(ctx context.Context, id string) error

	// Delete destroys an environment and frees all resources.
	Delete(ctx context.Context, id string) error

	// Name returns a human-readable identifier for this backend.
	Name() string
}
