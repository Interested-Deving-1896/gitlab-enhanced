package bandwidth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/bandwidth"
)

// startBandwidthService launches a bandwidth service on a fixed port and
// returns its base URL. The service is shut down when the test ends.
func startBandwidthService(t *testing.T, cfg bandwidth.Config) string {
	t.Helper()
	cfg.Enabled = true
	if cfg.DBPath == "" {
		cfg.DBPath = filepath.Join(t.TempDir(), "bandwidth.db")
	}
	port := 16200 + (len(t.Name()) % 900)
	cfg.ListenAddr = fmt.Sprintf("127.0.0.1:%d", port)
	if cfg.UpstreamGitLab == "" {
		cfg.UpstreamGitLab = "http://127.0.0.1:1" // unreachable — proxy not tested here
	}

	svc, err := bandwidth.New(cfg)
	if err != nil {
		t.Fatalf("bandwidth.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	ready := make(chan struct{})
	go func() {
		close(ready)
		_ = svc.Start(ctx)
	}()
	<-ready

	base := "http://" + cfg.ListenAddr
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/health")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(50 * time.Millisecond)
	}
	return base
}

func postJSONBW(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

// TestBWIntegration_Health verifies the /health endpoint.
func TestBWIntegration_Health(t *testing.T) {
	base := startBandwidthService(t, bandwidth.Config{})
	resp, err := http.Get(base + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// TestBWIntegration_ArtifactRegisterAndPolicy exercises the full
// register → policy endpoint flow over HTTP.
func TestBWIntegration_ArtifactRegisterAndPolicy(t *testing.T) {
	base := startBandwidthService(t, bandwidth.Config{ArtifactMaxSizeMB: 100})

	// Register an artifact
	resp := postJSONBW(t, base+"/artifacts/register", map[string]any{
		"path":       "/tmp/test-artifact.zip",
		"size_bytes": 1024 * 1024,
		"project_id": 1,
		"job_id":     42,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("register: expected 200, got %d: %s", resp.StatusCode, body)
	}

	// Check policy endpoint shows 1 tracked artifact
	resp2, err := http.Get(base + "/artifacts/policy")
	if err != nil {
		t.Fatalf("GET /artifacts/policy: %v", err)
	}
	defer resp2.Body.Close()
	var policy map[string]any
	body, _ := io.ReadAll(resp2.Body)
	if err := json.Unmarshal(body, &policy); err != nil {
		t.Fatalf("decode policy: %v", err)
	}
	if policy["artifacts_tracked"].(float64) != 1 {
		t.Errorf("artifacts_tracked: got %v, want 1", policy["artifacts_tracked"])
	}
}

// TestBWIntegration_ArtifactRegisterOversized verifies that registering an
// artifact exceeding the size limit returns HTTP 400.
func TestBWIntegration_ArtifactRegisterOversized(t *testing.T) {
	base := startBandwidthService(t, bandwidth.Config{ArtifactMaxSizeMB: 1})

	resp := postJSONBW(t, base+"/artifacts/register", map[string]any{
		"path":       "/tmp/huge.zip",
		"size_bytes": 10 * 1024 * 1024, // 10 MB > 1 MB limit
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("oversized artifact: expected 400, got %d", resp.StatusCode)
	}
}

// TestBWIntegration_StatsEndpoint verifies the /stats endpoint returns valid JSON.
func TestBWIntegration_StatsEndpoint(t *testing.T) {
	base := startBandwidthService(t, bandwidth.Config{})
	resp, err := http.Get(base + "/stats")
	if err != nil {
		t.Fatalf("GET /stats: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	var stats map[string]any
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &stats); err != nil {
		t.Fatalf("decode stats: %v\nbody: %s", err, body)
	}
	// dedup_bytes_saved_mb is always present regardless of traffic
	if _, ok := stats["dedup_bytes_saved_mb"]; !ok {
		t.Errorf("stats missing dedup_bytes_saved_mb field; got keys: %v", stats)
	}
}

// TestBWIntegration_ConcurrentArtifactRegistration verifies that concurrent
// artifact registrations do not corrupt the SQLite database.
func TestBWIntegration_ConcurrentArtifactRegistration(t *testing.T) {
	base := startBandwidthService(t, bandwidth.Config{ArtifactMaxSizeMB: 100})

	const n = 30
	var wg sync.WaitGroup
	errs := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			resp := postJSONBW(t, base+"/artifacts/register", map[string]any{
				"path":       fmt.Sprintf("/tmp/artifact-%d.zip", idx),
				"size_bytes": 1024,
				"job_id":     idx,
			})
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errs <- fmt.Errorf("goroutine %d: got %d", idx, resp.StatusCode)
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}

	// All artifacts should be tracked
	resp, err := http.Get(base + "/artifacts/policy")
	if err != nil {
		t.Fatalf("GET /artifacts/policy: %v", err)
	}
	defer resp.Body.Close()
	var policy map[string]any
	body, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(body, &policy)
	if policy["artifacts_tracked"].(float64) != float64(n) {
		t.Errorf("artifacts_tracked: got %v, want %d", policy["artifacts_tracked"], n)
	}
}
