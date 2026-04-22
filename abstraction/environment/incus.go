package environment

import (
	"context"
	"fmt"
)

// IncusManager provisions dev environments as Incus containers or VMs.
// This is the default local backend — no cloud account or Kubernetes required.
//
// Lifecycle per environment:
//  1. incus launch <image> <env-id> --profile <profile> --network <network>
//  2. incus exec <env-id> -- git clone <repoURL> /workspace/<repo>
//  3. incus exec <env-id> -- supervisor start (reads devcontainer.json / .gitpod.yml)
//  4. Incus proxy device exposes IDE port to host
//  5. Return IDEURL pointing at the proxied port
type IncusManager struct {
	socket  string
	profile string
	network string
	idePort int
}

func NewIncusManager(socket, profile, network string, idePort int) *IncusManager {
	return &IncusManager{
		socket:  socket,
		profile: profile,
		network: network,
		idePort: idePort,
	}
}

func (m *IncusManager) Name() string { return "incus-environment" }

func (m *IncusManager) Available(_ context.Context) bool {
	return m.socket != ""
}

func (m *IncusManager) Create(_ context.Context, spec Spec) (*Environment, error) {
	// TODO: implement via Incus REST API
	// See lifecycle comment above.
	return nil, fmt.Errorf("IncusManager.Create: not yet implemented (spec: %s)", spec.ID)
}

func (m *IncusManager) Get(_ context.Context, id string) (*Environment, error) {
	return nil, fmt.Errorf("IncusManager.Get: not yet implemented (id: %s)", id)
}

func (m *IncusManager) List(_ context.Context, _ Status) ([]*Environment, error) {
	return nil, fmt.Errorf("IncusManager.List: not yet implemented")
}

func (m *IncusManager) Stop(_ context.Context, id string) error {
	return fmt.Errorf("IncusManager.Stop: not yet implemented (id: %s)", id)
}

func (m *IncusManager) Start(_ context.Context, id string) error {
	return fmt.Errorf("IncusManager.Start: not yet implemented (id: %s)", id)
}

func (m *IncusManager) Delete(_ context.Context, id string) error {
	return fmt.Errorf("IncusManager.Delete: not yet implemented (id: %s)", id)
}

var _ Manager = (*IncusManager)(nil)
