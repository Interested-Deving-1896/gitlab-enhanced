package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// BlacksmithRunner dispatches CI jobs to Blacksmith's Firecracker-based runners.
// Used only when cloud.enabled=true. Communicates with the Blacksmith API.
//
// Self-hosted alternative: replace the Blacksmith API URL with a local
// scheduler backed by the K8s-in-Incus cluster (runtime/k8s-in-incus).
// See: https://github.com/useblacksmith
type BlacksmithRunner struct {
	org     string
	token   string
	apiURL  string
	client  *http.Client
}

func NewBlacksmithRunner(org, token string) *BlacksmithRunner {
	return &BlacksmithRunner{
		org:    org,
		token:  token,
		apiURL: "https://api.blacksmith.sh",
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// WithAPIURL overrides the Blacksmith API URL (for self-hosted scheduler).
func (r *BlacksmithRunner) WithAPIURL(url string) *BlacksmithRunner {
	r.apiURL = url
	return r
}

func (r *BlacksmithRunner) Name() string { return "blacksmith:" + r.org }

// Available checks that the Blacksmith API is reachable and the token is valid.
func (r *BlacksmithRunner) Available(ctx context.Context) bool {
	if r.org == "" || r.token == "" {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		r.apiURL+"/v1/orgs/"+r.org, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	resp, err := r.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Run dispatches a job to Blacksmith and polls for completion.
func (r *BlacksmithRunner) Run(ctx context.Context, job JobSpec, logs io.Writer) (*JobResult, error) {
	start := time.Now()

	// Dispatch job
	jobID, err := r.dispatch(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("blacksmith dispatch: %w", err)
	}
	fmt.Fprintf(logs, "[blacksmith] dispatched job %s → runner job %s\n", job.ID, jobID)

	// Poll for completion
	exitCode, err := r.poll(ctx, jobID, logs)
	if err != nil {
		return nil, fmt.Errorf("blacksmith poll: %w", err)
	}

	return &JobResult{
		JobID:         job.ID,
		ExitCode:      exitCode,
		Duration:      time.Since(start),
		ArtifactPaths: map[string]string{},
	}, nil
}

// dispatch submits a job to the Blacksmith API and returns the runner job ID.
func (r *BlacksmithRunner) dispatch(ctx context.Context, job JobSpec) (string, error) {
	payload := map[string]any{
		"org":      r.org,
		"job_id":   job.ID,
		"image":    job.Image,
		"commands": job.Commands,
		"env":      job.Env,
		"resources": map[string]any{
			"cpus":   job.Resources.CPUs,
			"memory": job.Resources.Memory,
		},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		r.apiURL+"/v1/jobs", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, respBody)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}
	return result.ID, nil
}

// poll waits for a Blacksmith job to complete, streaming log lines.
func (r *BlacksmithRunner) poll(ctx context.Context, jobID string, logs io.Writer) (int, error) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return 1, ctx.Err()
		case <-ticker.C:
			status, exitCode, logLine, err := r.getStatus(ctx, jobID)
			if err != nil {
				return 1, err
			}
			if logLine != "" {
				fmt.Fprintln(logs, logLine)
			}
			switch status {
			case "completed":
				return exitCode, nil
			case "failed":
				return exitCode, fmt.Errorf("job failed with exit code %d", exitCode)
			case "cancelled":
				return 1, fmt.Errorf("job was cancelled")
			}
			// "pending", "running" — keep polling
		}
	}
}

// getStatus fetches the current status of a Blacksmith job.
func (r *BlacksmithRunner) getStatus(ctx context.Context, jobID string) (status string, exitCode int, logLine string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		r.apiURL+"/v1/jobs/"+jobID, nil)
	if err != nil {
		return "", 1, "", err
	}
	req.Header.Set("Authorization", "Bearer "+r.token)

	resp, err := r.client.Do(req)
	if err != nil {
		return "", 1, "", err
	}
	defer resp.Body.Close()

	var result struct {
		Status   string `json:"status"`
		ExitCode int    `json:"exit_code"`
		LogLine  string `json:"log_line"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", 1, "", fmt.Errorf("decoding status: %w", err)
	}
	return result.Status, result.ExitCode, result.LogLine, nil
}

// Cancel sends a cancellation request for a running job.
func (r *BlacksmithRunner) Cancel(ctx context.Context, jobID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		r.apiURL+"/v1/jobs/"+jobID, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("cancel returned status %d", resp.StatusCode)
	}
	return nil
}

var _ Runner = (*BlacksmithRunner)(nil)
