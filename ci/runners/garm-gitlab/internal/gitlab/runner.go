package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// RunnerInfo holds the registration details returned by GitLab after a runner
// is registered. The Token is what gitlab-runner uses to poll for jobs.
type RunnerInfo struct {
	ID          int64    `json:"id"`
	Token       string   `json:"token"`
	Description string   `json:"description"`
	TagList     []string `json:"tag_list"`
	Active      bool     `json:"active"`
	Locked      bool     `json:"locked"`
}

// RunnerStatus is returned by GET /api/v4/runners/:id.
type RunnerStatus struct {
	ID          int64    `json:"id"`
	Description string   `json:"description"`
	Active      bool     `json:"active"`
	Online      bool     `json:"online"`
	Status      string   `json:"status"` // "online", "offline", "paused", "stale"
	TagList     []string `json:"tag_list"`
	RunUntagged bool     `json:"run_untagged"`
	Locked      bool     `json:"locked"`
	IPAddress   string   `json:"ip_address"`
	ContactedAt time.Time `json:"contacted_at"`
}

// RegistrationRequest is the body sent to POST /api/v4/runners.
type RegistrationRequest struct {
	Token       string   `json:"token"`        // registration token from GitLab project/group
	Description string   `json:"description"`
	TagList     string   `json:"tag_list"`     // comma-separated
	RunUntagged bool     `json:"run_untagged"`
	Locked      bool     `json:"locked"`
	MaxTimeout  int      `json:"maximum_timeout,omitempty"`
}

// Client is a minimal GitLab API client for runner lifecycle operations.
type Client struct {
	baseURL    string
	adminToken string // personal access token with manage_runner scope
	httpClient *http.Client
	log        *logrus.Logger
}

// NewClient creates a GitLab API client.
// baseURL is e.g. "https://gitlab.com".
// adminToken is a PAT with api + manage_runner scopes.
func NewClient(baseURL, adminToken string, log *logrus.Logger) *Client {
	return &Client{
		baseURL:    baseURL,
		adminToken: adminToken,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		log:        log,
	}
}

// RegisterRunner registers a new gitlab-runner instance and returns its
// RunnerInfo including the runner token used for job polling.
//
// registrationToken is the project or group runner registration token from
// GitLab UI → Settings → CI/CD → Runners.
func (c *Client) RegisterRunner(ctx context.Context, registrationToken, description, tagList string, runUntagged bool) (*RunnerInfo, error) {
	req := RegistrationRequest{
		Token:       registrationToken,
		Description: description,
		TagList:     tagList,
		RunUntagged: runUntagged,
		Locked:      true, // lock to the project that registered it
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal registration request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx,
		http.MethodPost,
		c.baseURL+"/api/v4/runners",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// Registration uses the registration token, not the admin PAT.

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("register runner: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("register runner: HTTP %d: %s", resp.StatusCode, string(b))
	}

	var info RunnerInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode runner info: %w", err)
	}

	c.log.WithFields(logrus.Fields{
		"runner_id":   info.ID,
		"description": info.Description,
		"tags":        tagList,
	}).Info("runner registered")

	return &info, nil
}

// DeleteRunner deregisters a runner by its token (the runner token, not the
// registration token). Called when an Incus instance is being destroyed.
func (c *Client) DeleteRunner(ctx context.Context, runnerToken string) error {
	body, _ := json.Marshal(map[string]string{"token": runnerToken})

	httpReq, err := http.NewRequestWithContext(ctx,
		http.MethodDelete,
		c.baseURL+"/api/v4/runners",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("create delete request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("delete runner: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete runner: HTTP %d: %s", resp.StatusCode, string(b))
	}

	c.log.WithField("token_prefix", runnerToken[:8]+"...").Info("runner deregistered")
	return nil
}

// GetRunnerStatus returns the current status of a runner by its numeric ID.
// Requires the admin PAT with read_api or api scope.
func (c *Client) GetRunnerStatus(ctx context.Context, runnerID int64) (*RunnerStatus, error) {
	httpReq, err := http.NewRequestWithContext(ctx,
		http.MethodGet,
		fmt.Sprintf("%s/api/v4/runners/%d", c.baseURL, runnerID),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("PRIVATE-TOKEN", c.adminToken)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("get runner status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get runner status: HTTP %d: %s", resp.StatusCode, string(b))
	}

	var status RunnerStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decode runner status: %w", err)
	}

	return &status, nil
}

// ListIdleRunners returns runner IDs that are online but not currently
// processing a job. Used by the pool manager to decide which instances to
// scale down.
func (c *Client) ListIdleRunners(ctx context.Context, groupID int64) ([]RunnerStatus, error) {
	url := fmt.Sprintf("%s/api/v4/groups/%d/runners?status=online&per_page=100", c.baseURL, groupID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("PRIVATE-TOKEN", c.adminToken)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("list runners: %w", err)
	}
	defer resp.Body.Close()

	var runners []RunnerStatus
	if err := json.NewDecoder(resp.Body).Decode(&runners); err != nil {
		return nil, fmt.Errorf("decode runners: %w", err)
	}

	// Filter to idle only (online but not running a job).
	idle := runners[:0]
	for _, r := range runners {
		if r.Status == "online" {
			idle = append(idle, r)
		}
	}
	return idle, nil
}
