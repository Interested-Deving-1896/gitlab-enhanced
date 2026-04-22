package environment

import (
	"context"
	"fmt"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/config"
)

// GitpodK8sManager delegates environment creation to a Gitpod Classic instance
// running on the K8s cluster provisioned by runtime/k8s-in-incus.
//
// This enables the full Gitpod Classic experience (multi-user dashboard,
// auth providers, workspace snapshots) without any external cloud dependency.
// The K8s cluster itself runs inside Incus VMs on the local host.
//
// Prerequisites:
//   - K8s cluster provisioned: cd runtime/k8s-in-incus && ansible-playbook ansible/playbooks/k8s-cluster.yml
//   - Gitpod Classic installed: ansible-playbook ansible/playbooks/k8s-gitpod.yml
type GitpodK8sManager struct {
	apiURL    string
	authToken string
}

func NewGitpodK8sManager(cfg *config.Config) *GitpodK8sManager {
	return &GitpodK8sManager{
		apiURL:    "https://" + cfg.GitLab.Domain + ":8443",
		authToken: cfg.Environment.Token,
	}
}

func (m *GitpodK8sManager) Name() string { return "gitpod-k8s:" + m.apiURL }

func (m *GitpodK8sManager) Available(_ context.Context) bool {
	// TODO: GET /api/v1/workspaces health check against Gitpod server
	return m.apiURL != ""
}

func (m *GitpodK8sManager) Create(_ context.Context, spec Spec) (*Environment, error) {
	// TODO: POST to Gitpod Classic API to start workspace
	// Maps Spec.RepoURL → Gitpod workspace context URL
	return nil, fmt.Errorf("GitpodK8sManager.Create: not yet implemented (spec: %s)", spec.ID)
}

func (m *GitpodK8sManager) Get(_ context.Context, id string) (*Environment, error) {
	return nil, fmt.Errorf("GitpodK8sManager.Get: not yet implemented (id: %s)", id)
}

func (m *GitpodK8sManager) List(_ context.Context, _ Status) ([]*Environment, error) {
	return nil, fmt.Errorf("GitpodK8sManager.List: not yet implemented")
}

func (m *GitpodK8sManager) Stop(_ context.Context, id string) error {
	return fmt.Errorf("GitpodK8sManager.Stop: not yet implemented (id: %s)", id)
}

func (m *GitpodK8sManager) Start(_ context.Context, id string) error {
	return fmt.Errorf("GitpodK8sManager.Start: not yet implemented (id: %s)", id)
}

func (m *GitpodK8sManager) Delete(_ context.Context, id string) error {
	return fmt.Errorf("GitpodK8sManager.Delete: not yet implemented (id: %s)", id)
}

var _ Manager = (*GitpodK8sManager)(nil)
