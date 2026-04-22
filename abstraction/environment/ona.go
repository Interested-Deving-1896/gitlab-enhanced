package environment

import (
	"context"
	"fmt"
)

// OnaManager delegates environment creation to Ona (formerly Gitpod) cloud.
// Used only when cloud.enabled=true and environment.backend="ona".
//
// Ona provides managed ephemeral environments with VS Code in the browser,
// agent capabilities, and guardrails. See: https://ona.com
type OnaManager struct {
	token string
}

func NewOnaManager(token string) *OnaManager {
	return &OnaManager{token: token}
}

func (m *OnaManager) Name() string { return "ona" }

func (m *OnaManager) Available(_ context.Context) bool {
	// TODO: check Ona API reachability
	return m.token != ""
}

func (m *OnaManager) Create(_ context.Context, spec Spec) (*Environment, error) {
	// TODO: POST to Ona API to create environment
	// https://ona.com/docs/ona/api
	return nil, fmt.Errorf("OnaManager.Create: not yet implemented (spec: %s)", spec.ID)
}

func (m *OnaManager) Get(_ context.Context, id string) (*Environment, error) {
	return nil, fmt.Errorf("OnaManager.Get: not yet implemented (id: %s)", id)
}

func (m *OnaManager) List(_ context.Context, _ Status) ([]*Environment, error) {
	return nil, fmt.Errorf("OnaManager.List: not yet implemented")
}

func (m *OnaManager) Stop(_ context.Context, id string) error {
	return fmt.Errorf("OnaManager.Stop: not yet implemented (id: %s)", id)
}

func (m *OnaManager) Start(_ context.Context, id string) error {
	return fmt.Errorf("OnaManager.Start: not yet implemented (id: %s)", id)
}

func (m *OnaManager) Delete(_ context.Context, id string) error {
	return fmt.Errorf("OnaManager.Delete: not yet implemented (id: %s)", id)
}

var _ Manager = (*OnaManager)(nil)
