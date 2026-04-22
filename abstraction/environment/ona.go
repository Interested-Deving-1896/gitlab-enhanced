package environment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OnaManager delegates environment creation to Ona (formerly Gitpod) cloud.
// Used only when cloud.enabled=true and environment.backend="ona".
//
// Ona provides managed ephemeral environments with VS Code in the browser,
// agent capabilities, and guardrails. See: https://ona.com
type OnaManager struct {
	token  string
	apiURL string
	client *http.Client
}

func NewOnaManager(token string) *OnaManager {
	return &OnaManager{
		token:  token,
		apiURL: "https://app.ona.com/api/v1",
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (m *OnaManager) Name() string { return "ona" }

// Available checks that the Ona API is reachable and the token is valid.
func (m *OnaManager) Available(ctx context.Context) bool {
	if m.token == "" {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		m.apiURL+"/user", nil)
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

// Create provisions a new Ona environment for the given repo.
func (m *OnaManager) Create(ctx context.Context, spec Spec) (*Environment, error) {
	payload := map[string]any{
		"contextUrl": onaContextURL(spec),
		"ideSettings": map[string]any{
			"defaultIde": onaIDE(spec.IDE),
		},
	}
	if spec.Resources.CPUs > 0 || spec.Resources.Memory != "" {
		payload["workspaceClass"] = onaWorkspaceClass(spec.Resources)
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		m.apiURL+"/environments", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	m.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ona create environment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ona create environment: status %d: %s", resp.StatusCode, respBody)
	}

	var env onaEnvironment
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, fmt.Errorf("decoding environment response: %w", err)
	}

	// Poll until running
	result, err := m.waitRunning(ctx, env.ID)
	if err != nil {
		return nil, err
	}
	result.Spec = spec
	return result, nil
}

// Get returns the current state of an Ona environment.
func (m *OnaManager) Get(ctx context.Context, id string) (*Environment, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		m.apiURL+"/environments/"+id, nil)
	if err != nil {
		return nil, err
	}
	m.setAuth(req)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ona get environment %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("environment %s not found", id)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ona get environment %s: status %d: %s", id, resp.StatusCode, body)
	}

	var env onaEnvironment
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, fmt.Errorf("decoding environment: %w", err)
	}
	return env.toEnvironment(), nil
}

// List returns all Ona environments.
func (m *OnaManager) List(ctx context.Context, status Status) ([]*Environment, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		m.apiURL+"/environments", nil)
	if err != nil {
		return nil, err
	}
	m.setAuth(req)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ona list environments: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Environments []onaEnvironment `json:"environments"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding environments: %w", err)
	}

	var envs []*Environment
	for _, e := range result.Environments {
		env := e.toEnvironment()
		if status == "" || env.Status == status {
			envs = append(envs, env)
		}
	}
	return envs, nil
}

// Stop sends a stop request for an Ona environment.
func (m *OnaManager) Stop(ctx context.Context, id string) error {
	return m.action(ctx, id, "stop")
}

// Start sends a start request for a stopped Ona environment.
func (m *OnaManager) Start(ctx context.Context, id string) error {
	return m.action(ctx, id, "start")
}

// Delete permanently deletes an Ona environment.
func (m *OnaManager) Delete(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		m.apiURL+"/environments/"+id, nil)
	if err != nil {
		return err
	}
	m.setAuth(req)
	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("ona delete environment %s: %w", id, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("ona delete environment %s: status %d", id, resp.StatusCode)
	}
	return nil
}

func (m *OnaManager) action(ctx context.Context, id, action string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/environments/%s/%s", m.apiURL, id, action), nil)
	if err != nil {
		return err
	}
	m.setAuth(req)
	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("ona %s environment %s: %w", action, id, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ona %s environment %s: status %d", action, id, resp.StatusCode)
	}
	return nil
}

func (m *OnaManager) waitRunning(ctx context.Context, id string) (*Environment, error) {
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
				return nil, fmt.Errorf("environment %s entered error state", id)
			}
		}
	}
}

func (m *OnaManager) setAuth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+m.token)
}

// onaEnvironment is the Ona API environment representation.
type onaEnvironment struct {
	ID     string `json:"id"`
	Status string `json:"status"` // running, stopped, starting, error
	IDEURL string `json:"ideUrl"`
	Spec   struct {
		ContextURL string `json:"contextUrl"`
	} `json:"spec"`
}

func (e *onaEnvironment) toEnvironment() *Environment {
	status := StatusStopped
	switch e.Status {
	case "running":
		status = StatusRunning
	case "starting", "provisioning":
		status = StatusStarting
	case "stopped", "stopping":
		status = StatusStopped
	default:
		status = StatusError
	}
	return &Environment{
		ID:     e.ID,
		Status: status,
		IDEURL: e.IDEURL,
	}
}

// onaContextURL builds the context URL from a Spec.
func onaContextURL(spec Spec) string {
	if spec.Branch != "" {
		return spec.RepoURL + "/tree/" + spec.Branch
	}
	return spec.RepoURL
}

// onaIDE maps our IDE name to Ona's IDE identifier.
func onaIDE(ide string) string {
	switch ide {
	case "openvscode-server", "":
		return "code"
	default:
		return ide
	}
}

// onaWorkspaceClass maps resource requirements to an Ona workspace class.
func onaWorkspaceClass(r ResourceSpec) string {
	if r.CPUs >= 8 {
		return "large"
	}
	if r.CPUs >= 4 {
		return "medium"
	}
	return "small"
}

var _ Manager = (*OnaManager)(nil)
