// Package build defines the backend-agnostic container image build interface.
// Implementations: Incus+BuildKit (default/local), Depot (cloud secondary).
package build

import (
	"context"
	"io"
)

// BuildRequest describes a container image build.
type BuildRequest struct {
	// ContextDir is the path to the build context (directory containing Dockerfile).
	ContextDir string

	// Dockerfile is the path to the Dockerfile, relative to ContextDir.
	// Defaults to "Dockerfile".
	Dockerfile string

	// Tags are the image references to tag the result with.
	Tags []string

	// BuildArgs are --build-arg key=value pairs.
	BuildArgs map[string]string

	// Platforms lists target platforms (e.g. "linux/amd64", "linux/arm64").
	// Empty means the builder's native platform.
	Platforms []string

	// Push, if true, pushes the image to the registry after building.
	Push bool

	// CacheFrom lists image references to use as cache sources.
	CacheFrom []string
}

// BuildResult is returned on successful build completion.
type BuildResult struct {
	// ImageID is the content-addressable digest of the built image.
	ImageID string

	// Tags are the tags applied to the image.
	Tags []string

	// BuildDuration is the wall-clock time the build took.
	BuildDuration string
}

// Builder is the build backend interface.
type Builder interface {
	// Available reports whether this builder is reachable.
	Available(ctx context.Context) bool

	// Build executes a container image build.
	// logs receives build output lines; close it when done.
	Build(ctx context.Context, req BuildRequest, logs io.Writer) (*BuildResult, error)

	// Name returns a human-readable identifier.
	Name() string
}
