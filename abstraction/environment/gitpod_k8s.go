package environment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/config"
)

// GitpodK8sManager delegates environment creation to a Gitpod Classic instance
// running on the K8s cluster provisioned by runtime/k8s-in-incus.
//
// Gitpod Classic exposes a JSON-RPC WebSocket API. This manager uses the
// REST-compatible HTTP endpoints for workspace lifecycle operations.
//
// Prerequisites:
//   - K8s cluster: ansible-playbook runtime/k8s-in-incus/ansible/playbooks/k8s-cluster.yml
//   - Gitpod Classic: ansible-playbook runtime/k8s-in-incus/ansible/playbooks/k8s-gitpod.yml
type GitpodK8sManager struct {
	apiURL    string
	authToken string
	client    *http.Client
}

func NewGitpodK8sManager(cfg *config.Config) *GitpodK8sManager {
	// Gitpod Classic runs on its own subdomain, not the GitLab domain.
	// Falls back to "gitpod.<gitlab-domain>" when gitpod_domain is unset.
	gitpodDomain := cfg.Environment.GitpodDomain
	if gitpodDomain == "" {
		gitpodDomain = "gitpod." + cfg.GitLab.Domain
	}
	return &GitpodK8sManager{
		apiURL:    "https://" + gitpodDomain,
		authToken: cfg.Environment.Token,
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (m *GitpodK8sManager) Name() string { return "gitpod-k8s:" + m.apiURL }

// Available checks that the Gitpod Classic API is reachable.
func (m *GitpodK8sManager) Available(ctx context.Context) bool {
	if m.apiURL == "" || m.authToken == "" {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		m.apiURL+"/api/v1/workspaces", nil)
	if err != nil {
		return false
	}
	m.setAuth(req)
	resp, err := m.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Create starts a new Gitpod Classic workspace for the given repo.
func (m *GitpodK8sManager) Create(ctx context.Context, spec Spec) (*Environment, error) {
	// Gitpod Classic uses a context URL to identify the repo+branch
	contextURL := spec.RepoURL
	if spec.Branch != "" {
		contextURL = spec.RepoURL + "/tree/" + spec.Branch
	}

	payload := map[string]any{
		"contextUrl": contextURL,
		"startSpec": map[string]any{
			"ideSettings": map[string]any{
				"defaultIde": gitpodIDE(spec.IDE),
			},
		},
	}
	if spec.Resources.CPUs > 0 || spec.Resources.Memory != "" {
		payload["workspaceClass"] = "custom"
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		m.apiURL+"/api/v1/workspaces", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	m.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitpod create workspace: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gitpod create workspace: status %d: %s", resp.StatusCode, respBody)
	}

	var ws gitpodWorkspace
	if err := json.NewDecoder(resp.Body).Decode(&ws); err != nil {
		return nil, fmt.Errorf("decoding workspace response: %w", err)
	}

	// Poll until workspace is running
	env, err := m.waitRunning(ctx, ws.ID)
	if err != nil {
		return nil, err
	}
	env.Spec = spec
	return env, nil
}

// Get returns the current state of a Gitpod Classic workspace.
func (m *GitpodK8sManager) Get(ctx context.Context, id string) (*Environment, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		m.apiURL+"/api/v1/workspaces/"+id, nil)
	if err != nil {
		return nil, err
	}
	m.setAuth(req)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitpod get workspace %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("workspace %s not found", id)
	}

	var ws gitpodWorkspace
	if err := json.NewDecoder(resp.Body).Decode(&ws); err != nil {
		return nil, fmt.Errorf("decoding workspace: %w", err)
	}
	return ws.toEnvironment(), nil
}

// List returns all Gitpod Classic workspaces.
func (m *GitpodK8sManager) List(ctx context.Context, status Status) ([]*Environment, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		m.apiURL+"/api/v1/workspaces", nil)
	if err != nil {
		return nil, err
	}
	m.setAuth(req)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitpod list workspaces: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Workspaces []gitpodWorkspace `json:"workspaces"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding workspaces: %w", err)
	}

	var envs []*Environment
	for _, ws := range result.Workspaces {
		env := ws.toEnvironment()
		if status == "" || env.Status == status {
			envs = append(envs, env)
		}
	}
	return envs, nil
}

// Stop sends a stop request for a Gitpod Classic workspace.
func (m *GitpodK8sManager) Stop(ctx context.Context, id string) error {
	return m.action(ctx, id, "stop")
}

// Start sends a start request for a stopped Gitpod Classic workspace.
func (m *GitpodK8sManager) Start(ctx context.Context, id string) error {
	return m.action(ctx, id, "start")
}

// Delete permanently deletes a Gitpod Classic workspace.
func (m *GitpodK8sManager) Delete(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		m.apiURL+"/api/v1/workspaces/"+id, nil)
	if err != nil {
		return err
	}
	m.setAuth(req)
	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("gitpod delete workspace %s: %w", id, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("gitpod delete workspace %s: status %d", id, resp.StatusCode)
	}
	return nil
}

// action sends a lifecycle action (start/stop) to a workspace.
func (m *GitpodK8sManager) action(ctx context.Context, id, action string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/api/v1/workspaces/%s/%s", m.apiURL, id, action), nil)
	if err != nil {
		return err
	}
	m.setAuth(req)
	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("gitpod %s workspace %s: %w", action, id, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gitpod %s workspace %s: status %d", action, id, resp.StatusCode)
	}
	return nil
}

// waitRunning polls until the workspace reaches Running status.
func (m *GitpodK8sManager) waitRunning(ctx context.Context, id string) (*Environment, error) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			env, err := m.Get(ctx, id)
			if err != nil {
				return nil, err
			}
			if env.Status == StatusRunning {
				return env, nil
			}
			if env.Status == StatusError {
				return nil, fmt.Errorf("workspace %s entered error state", id)
			}
		}
	}
}

func (m *GitpodK8sManager) setAuth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+m.authToken)
}

// gitpodWorkspace is the Gitpod Classic API workspace representation.
type gitpodWorkspace struct {
	ID     string `json:"id"`
	Status struct {
		Phase string `json:"phase"` // RUNNING, STOPPED, STARTING, etc.
		URL   string `json:"url"`
	} `json:"status"`
	Context struct {
		Repository struct {
			CloneURL string `json:"cloneUrl"`
		} `json:"repository"`
		Ref string `json:"ref"`
	} `json:"context"`
}

func (ws *gitpodWorkspace) toEnvironment() *Environment {
	status := StatusStopped
	switch ws.Status.Phase {
	case "RUNNING":
		status = StatusRunning
	case "STARTING", "INITIALIZING", "PENDING":
		status = StatusStarting
	case "STOPPING", "STOPPED":
		status = StatusStopped
	default:
		status = StatusError
	}
	return &Environment{
		ID:     ws.ID,
		Status: status,
		IDEURL: ws.Status.URL,
		Spec: Spec{
			RepoURL: ws.Context.Repository.CloneURL,
			Branch:  ws.Context.Ref,
		},
	}
}

// gitpodIDE maps our IDE name to Gitpod Classic's IDE identifier.
func gitpodIDE(ide string) string {
	switch ide {
	case "openvscode-server", "":
		return "code"
	case "jetbrains-idea":
		return "intellij"
	case "jetbrains-goland":
		return "goland"
	default:
		return "code"
	}
}

var _ Manager = (*GitpodK8sManager)(nil)
