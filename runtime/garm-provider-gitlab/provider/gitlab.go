// SPDX-License-Identifier: Apache-2.0
//
// Package provider implements the GARM ExternalProvider interface for GitLab.
//
// Each CreateInstance call:
//  1. Requests a JIT runner token from the GitLab API
//     (POST /api/v4/user/runners or /api/v4/projects/:id/runners)
//  2. Launches an ephemeral Incus container with cloud-init user-data that
//     installs gitlab-runner, registers with the JIT token (--ephemeral),
//     and runs one job before exiting
//  3. Returns a ProviderInstance to GARM
//
// DeleteInstance destroys the Incus container. GetInstance and ListInstances
// query Incus directly — no GitLab API calls needed for lifecycle polling.

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	commonParams "github.com/cloudbase/garm-provider-common/params"
	incus "github.com/lxc/incus/v6/client"
	incusAPI "github.com/lxc/incus/v6/shared/api"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/runtime/garm-provider-gitlab/config"
)

const providerVersion = "v0.1.0"

// controllerKey is stored in each Incus instance's config so we can
// distinguish our containers from others on the same host.
const controllerKey = "user.garm-controller-id"
const poolKey = "user.garm-pool-id"

// GitLabProvider implements garm-provider-common/execution/common.ExternalProvider.
type GitLabProvider struct {
	cfg          *config.Config
	controllerID string
	incus        incus.InstanceServer
	httpClient   *http.Client
}

// New creates a GitLabProvider from the config file at configPath.
func New(configPath, controllerID string) (*GitLabProvider, error) {
	cfg, err := config.NewConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	srv, err := incus.ConnectIncusUnix(cfg.IncusSocket, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to Incus at %s: %w", cfg.IncusSocket, err)
	}

	return &GitLabProvider{
		cfg:          cfg,
		controllerID: controllerID,
		incus:        srv,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// GetVersion returns the provider version string.
func (p *GitLabProvider) GetVersion(_ context.Context) string {
	return providerVersion
}

// CreateInstance provisions an ephemeral Incus container registered as a
// GitLab JIT runner.
func (p *GitLabProvider) CreateInstance(ctx context.Context, params commonParams.BootstrapInstance) (commonParams.ProviderInstance, error) {
	// Step 1: obtain a JIT runner token from GitLab
	jitToken, err := p.requestJITToken(ctx, params.Name)
	if err != nil {
		return commonParams.ProviderInstance{}, fmt.Errorf("request JIT token: %w", err)
	}

	// Step 2: build cloud-init user-data
	userData := p.buildUserData(params.Name, jitToken)

	// Step 3: launch Incus container
	req := incusAPI.InstancesPost{
		Name: params.Name,
		Type: incusAPI.InstanceTypeContainer,
		Source: incusAPI.InstanceSource{
			Type:  "image",
			Alias: p.cfg.IncusImage,
		},
		InstancePut: incusAPI.InstancePut{
			Profiles: []string{p.cfg.IncusProfile},
			Config: map[string]string{
				controllerKey:       p.controllerID,
				poolKey:             params.PoolID,
				"cloud-init.user-data": userData,
				"user.os-type":        string(params.OSType),
				"user.os-arch":        string(params.OSArch),
			},
			Ephemeral: true,
		},
	}

	op, err := p.incus.CreateInstance(req)
	if err != nil {
		return commonParams.ProviderInstance{}, fmt.Errorf("create Incus instance %s: %w", params.Name, err)
	}
	if err := op.WaitContext(ctx); err != nil {
		return commonParams.ProviderInstance{}, fmt.Errorf("wait for instance creation %s: %w", params.Name, err)
	}

	// Step 4: start the container
	startReq := incusAPI.InstanceStatePut{Action: "start", Timeout: 60}
	op, err = p.incus.UpdateInstanceState(params.Name, startReq, "")
	if err != nil {
		return commonParams.ProviderInstance{}, fmt.Errorf("start instance %s: %w", params.Name, err)
	}
	if err := op.WaitContext(ctx); err != nil {
		return commonParams.ProviderInstance{}, fmt.Errorf("wait for instance start %s: %w", params.Name, err)
	}

	return commonParams.ProviderInstance{
		ProviderID: params.Name,
		Name:       params.Name,
		OSType:     params.OSType,
		OSArch:     params.OSArch,
		Status:     commonParams.InstanceRunning,
	}, nil
}

// DeleteInstance destroys the named Incus container.
func (p *GitLabProvider) DeleteInstance(ctx context.Context, instance string) error {
	// Stop first (force)
	stopReq := incusAPI.InstanceStatePut{Action: "stop", Timeout: 30, Force: true}
	op, err := p.incus.UpdateInstanceState(instance, stopReq, "")
	if err == nil {
		_ = op.WaitContext(ctx) // best-effort
	}

	op, err = p.incus.DeleteInstance(instance)
	if err != nil {
		return fmt.Errorf("delete instance %s: %w", instance, err)
	}
	return op.WaitContext(ctx)
}

// GetInstance returns the current state of a single Incus container.
func (p *GitLabProvider) GetInstance(_ context.Context, instance string) (commonParams.ProviderInstance, error) {
	inst, _, err := p.incus.GetInstance(instance)
	if err != nil {
		return commonParams.ProviderInstance{}, fmt.Errorf("get instance %s: %w", instance, err)
	}
	return incusInstanceToProvider(inst), nil
}

// ListInstances returns all containers managed by this controller+pool.
func (p *GitLabProvider) ListInstances(_ context.Context, poolID string) ([]commonParams.ProviderInstance, error) {
	all, err := p.incus.GetInstances(incusAPI.InstanceTypeContainer)
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}

	var result []commonParams.ProviderInstance
	for _, inst := range all {
		if inst.Config[controllerKey] != p.controllerID {
			continue
		}
		if poolID != "" && inst.Config[poolKey] != poolID {
			continue
		}
		result = append(result, incusInstanceToProvider(&inst))
	}
	return result, nil
}

// RemoveAllInstances deletes every container owned by this controller.
func (p *GitLabProvider) RemoveAllInstances(ctx context.Context) error {
	instances, err := p.ListInstances(ctx, "")
	if err != nil {
		return err
	}
	var errs []string
	for _, inst := range instances {
		if err := p.DeleteInstance(ctx, inst.ProviderID); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", inst.ProviderID, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors removing instances: %s", strings.Join(errs, "; "))
	}
	return nil
}

// Stop halts a running container without deleting it.
func (p *GitLabProvider) Stop(ctx context.Context, instance string, force bool) error {
	req := incusAPI.InstanceStatePut{Action: "stop", Timeout: 30, Force: force}
	op, err := p.incus.UpdateInstanceState(instance, req, "")
	if err != nil {
		return fmt.Errorf("stop instance %s: %w", instance, err)
	}
	return op.WaitContext(ctx)
}

// Start boots a stopped container.
func (p *GitLabProvider) Start(ctx context.Context, instance string) error {
	req := incusAPI.InstanceStatePut{Action: "start", Timeout: 60}
	op, err := p.incus.UpdateInstanceState(instance, req, "")
	if err != nil {
		return fmt.Errorf("start instance %s: %w", instance, err)
	}
	return op.WaitContext(ctx)
}

// ── GitLab JIT token ──────────────────────────────────────────────────────────

type jitTokenRequest struct {
	RunnerType  string   `json:"runner_type"`
	ProjectID   int64    `json:"project_id,omitempty"`
	GroupID     int64    `json:"group_id,omitempty"`
	Description string   `json:"description"`
	TagList     []string `json:"tag_list"`
	RunUntagged bool     `json:"run_untagged"`
	Locked      bool     `json:"locked"`
}

type jitTokenResponse struct {
	Token          string `json:"token"`
	TokenExpiresAt string `json:"token_expires_at"`
}

// requestJITToken calls the GitLab API to obtain a JIT runner registration token.
// GitLab >= 16.0 required. The token is single-use and expires after 1 hour.
func (p *GitLabProvider) requestJITToken(ctx context.Context, runnerName string) (string, error) {
	reqBody := jitTokenRequest{
		Description: runnerName,
		TagList:     p.cfg.RunnerTags,
		RunUntagged: false,
		Locked:      false,
	}

	if p.cfg.ProjectID != 0 {
		reqBody.RunnerType = "project_type"
		reqBody.ProjectID = p.cfg.ProjectID
	} else {
		reqBody.RunnerType = "group_type"
		reqBody.GroupID = p.cfg.GroupID
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/api/v4/user/runners", strings.TrimRight(p.cfg.GitLabURL, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("PRIVATE-TOKEN", p.cfg.GitLabToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GitLab API request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("GitLab API returned %d: %s", resp.StatusCode, respBody)
	}

	var tokenResp jitTokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return "", fmt.Errorf("parse JIT token response: %w", err)
	}
	if tokenResp.Token == "" {
		return "", fmt.Errorf("GitLab returned empty JIT token")
	}

	return tokenResp.Token, nil
}

// ── cloud-init user-data ──────────────────────────────────────────────────────

// buildUserData returns a cloud-init user-data script that installs
// gitlab-runner, registers with the JIT token, and runs one ephemeral job.
func (p *GitLabProvider) buildUserData(runnerName, jitToken string) string {
	gitlabURL := strings.TrimRight(p.cfg.GitLabURL, "/")
	tags := strings.Join(p.cfg.RunnerTags, ",")

	return fmt.Sprintf(`#cloud-config
package_update: true
packages:
  - curl
  - ca-certificates
  - git

runcmd:
  # Install gitlab-runner
  - curl -fsSL https://packages.gitlab.com/install/repositories/runner/gitlab-runner/script.deb.sh | bash
  - apt-get install -y gitlab-runner

  # Register as an ephemeral runner using the JIT token.
  # --ephemeral: runner deregisters itself after completing one job.
  - |
    gitlab-runner register \
      --non-interactive \
      --url "%s" \
      --token "%s" \
      --executor shell \
      --description "%s" \
      --tag-list "%s" \
      --run-untagged false \
      --ephemeral

  # Start the runner (exits after one job due to --ephemeral)
  - gitlab-runner run --working-directory /builds --user gitlab-runner
`, gitlabURL, jitToken, runnerName, tags)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func incusInstanceToProvider(inst *incusAPI.Instance) commonParams.ProviderInstance {
	status := commonParams.InstancePendingCreate
	switch inst.Status {
	case "Running":
		status = commonParams.InstanceRunning
	case "Stopped":
		status = commonParams.InstanceStopped
	case "Error":
		status = commonParams.InstanceError
	}

	osType := commonParams.OSType(inst.Config["user.os-type"])
	if osType == "" {
		osType = commonParams.Linux
	}
	osArch := commonParams.OSArch(inst.Config["user.os-arch"])
	if osArch == "" {
		osArch = commonParams.Amd64
	}

	return commonParams.ProviderInstance{
		ProviderID: inst.Name,
		Name:       inst.Name,
		OSType:     osType,
		OSArch:     osArch,
		Status:     status,
	}
}
