package runner

import (
	"context"
	"fmt"
	"io"
)

// BlacksmithRunner delegates CI jobs to Blacksmith's Firecracker-based runners.
// Used only when cloud.enabled=true.
//
// Blacksmith provides fast microVM-based GitHub Actions runners.
// Self-hosted path: replace Blacksmith's cloud control plane with
// the Incus-based scheduler in runtime/k8s-in-incus.
// See: https://github.com/useblacksmith
type BlacksmithRunner struct {
	org   string
	token string
}

func NewBlacksmithRunner(org, token string) *BlacksmithRunner {
	return &BlacksmithRunner{org: org, token: token}
}

func (r *BlacksmithRunner) Name() string { return "blacksmith:" + r.org }

func (r *BlacksmithRunner) Available(_ context.Context) bool {
	return r.org != "" && r.token != ""
}

func (r *BlacksmithRunner) Run(_ context.Context, job JobSpec, logs io.Writer) (*JobResult, error) {
	fmt.Fprintf(logs, "BlacksmithRunner.Run: not yet implemented (job: %s)\n", job.ID)
	return nil, fmt.Errorf("BlacksmithRunner.Run: not yet implemented")
}

func (r *BlacksmithRunner) Cancel(_ context.Context, jobID string) error {
	return fmt.Errorf("BlacksmithRunner.Cancel: not yet implemented")
}

var _ Runner = (*BlacksmithRunner)(nil)
