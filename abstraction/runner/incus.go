package runner

import (
	"context"
	"fmt"
	"io"
)

// IncusRunner executes CI jobs inside Incus VMs.
// This is the default local runner — no cloud account required.
//
// Each job gets a fresh ephemeral VM launched from the configured profile.
// The VM is destroyed after the job completes (pass or fail).
//
// Architecture:
//   1. incus launch <image> <job-vm-name> --profile <vmProfile> --ephemeral
//   2. incus exec <job-vm-name> -- <commands>
//   3. incus file pull <job-vm-name>/<artifact> ./ (collect artifacts)
//   4. VM auto-destroyed on stop (ephemeral flag)
type IncusRunner struct {
	socket    string
	vmProfile string
	network   string
}

func NewIncusRunner(socket, vmProfile, network string) *IncusRunner {
	return &IncusRunner{socket: socket, vmProfile: vmProfile, network: network}
}

func (r *IncusRunner) Name() string { return "incus-runner:" + r.vmProfile }

func (r *IncusRunner) Available(_ context.Context) bool {
	return r.socket != ""
}

func (r *IncusRunner) Run(_ context.Context, job JobSpec, logs io.Writer) (*JobResult, error) {
	// TODO: implement via Incus REST API
	// See architecture comment above.
	fmt.Fprintf(logs, "IncusRunner.Run: not yet implemented (job: %s)\n", job.ID)
	return nil, fmt.Errorf("IncusRunner.Run: not yet implemented")
}

func (r *IncusRunner) Cancel(_ context.Context, jobID string) error {
	// TODO: incus stop <jobID> --force
	return fmt.Errorf("IncusRunner.Cancel: not yet implemented")
}

var _ Runner = (*IncusRunner)(nil)
