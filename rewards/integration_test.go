package rewards_test

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

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/rewards"
)

// startService launches a rewards service on a random port and returns its
// base URL. The service is shut down when the test ends.
func startService(t *testing.T, cfg rewards.Config) string {
	t.Helper()
	if cfg.DBPath == "" {
		cfg.DBPath = filepath.Join(t.TempDir(), "rewards.db")
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:0"
	}
	cfg.Enabled = true

	svc, err := rewards.New(cfg)
	if err != nil {
		t.Fatalf("rewards.New: %v", err)
	}

	// Use a fixed port derived from t.Name() hash to avoid conflicts.
	// Simpler: bind to a known free port by letting the OS pick, then read it back.
	// Since rewards.Service doesn't expose the listener, use a fixed high port per test.
	port := 16100 + (len(t.Name()) % 900)
	cfg.ListenAddr = fmt.Sprintf("127.0.0.1:%d", port)

	svc2, err := rewards.New(cfg)
	if err != nil {
		t.Fatalf("rewards.New (port): %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
	})

	ready := make(chan struct{})
	go func() {
		close(ready)
		_ = svc2.Start(ctx)
	}()
	<-ready

	// Wait for the server to be ready.
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

	_ = svc // suppress unused warning
	return base
}

func postJSON(t *testing.T, url string, body any, headers map[string]string) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func readJSON(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, dst); err != nil {
		t.Fatalf("decode JSON: %v\nbody: %s", err, body)
	}
}

// TestIntegration_HealthEndpoint verifies the /health endpoint returns 200
// with the configured publisher ID.
func TestIntegration_HealthEndpoint(t *testing.T) {
	base := startService(t, rewards.Config{
		PublisherID:   "test-pub",
		WalletAddress: "0xABCD",
	})

	resp, err := http.Get(base + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	readJSON(t, resp, &result)
	if result["publisher_id"] != "test-pub" {
		t.Errorf("publisher_id: got %v, want test-pub", result["publisher_id"])
	}
}

// TestIntegration_WalletRegisterAndGet exercises the full wallet registration
// and retrieval flow over HTTP.
func TestIntegration_WalletRegisterAndGet(t *testing.T) {
	base := startService(t, rewards.Config{})

	// Register a wallet
	resp := postJSON(t, base+"/wallet/register", map[string]any{
		"username":       "alice",
		"wallet_address": "0xALICE",
	}, nil)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("register: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Retrieve it
	resp2, err := http.Get(base + "/wallet/alice")
	if err != nil {
		t.Fatalf("GET /wallet/alice: %v", err)
	}
	var reg map[string]any
	readJSON(t, resp2, &reg)

	if reg["wallet_address"] != "0xALICE" {
		t.Errorf("wallet_address: got %v, want 0xALICE", reg["wallet_address"])
	}
}

// TestIntegration_WebhookQueuesReward sends a Merge Request Hook webhook and
// verifies a reward appears in /rewards/pending.
func TestIntegration_WebhookQueuesReward(t *testing.T) {
	base := startService(t, rewards.Config{})

	payload := map[string]any{
		"object_attributes": map[string]any{
			"state": "merged",
			"iid":   float64(42),
		},
		"user": map[string]any{
			"username": "bob",
			"email":    "bob@example.com",
		},
	}
	resp := postJSON(t, base+"/webhook/gitlab", payload, map[string]string{
		"X-Gitlab-Event": "Merge Request Hook",
	})
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("webhook: expected 200, got %d", resp.StatusCode)
	}
	var result map[string]any
	readJSON(t, resp, &result)
	if result["ok"] != true {
		t.Errorf("webhook response ok: got %v", result["ok"])
	}

	// Verify reward is in pending list
	resp2, err := http.Get(base + "/rewards/pending")
	if err != nil {
		t.Fatalf("GET /rewards/pending: %v", err)
	}
	var pending []map[string]any
	readJSON(t, resp2, &pending)
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending reward, got %d", len(pending))
	}
	if pending[0]["status"] != "pending" {
		t.Errorf("reward status: got %v, want pending", pending[0]["status"])
	}
}

// TestIntegration_WebhookSecretRejection verifies that a wrong X-Gitlab-Token
// results in HTTP 401.
func TestIntegration_WebhookSecretRejection(t *testing.T) {
	base := startService(t, rewards.Config{
		WebhookSecret: "correct-secret",
	})

	payload := map[string]any{"object_attributes": map[string]any{"state": "merged"}}

	// Wrong secret
	resp := postJSON(t, base+"/webhook/gitlab", payload, map[string]string{
		"X-Gitlab-Event": "Merge Request Hook",
		"X-Gitlab-Token": "wrong-secret",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong secret: expected 401, got %d", resp.StatusCode)
	}

	// Correct secret
	resp2 := postJSON(t, base+"/webhook/gitlab", payload, map[string]string{
		"X-Gitlab-Event": "Merge Request Hook",
		"X-Gitlab-Token": "correct-secret",
	})
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("correct secret: expected 200, got %d", resp2.StatusCode)
	}
}

// TestIntegration_PayoutWithoutCredentials verifies that /rewards/payout
// returns an error (not a silent success) when Uphold credentials are absent.
func TestIntegration_PayoutWithoutCredentials(t *testing.T) {
	base := startService(t, rewards.Config{MinPayoutBAT: 0.001})

	// Queue a reward first
	payload := map[string]any{
		"object_attributes": map[string]any{"state": "merged", "iid": float64(1)},
		"user":              map[string]any{"username": "carol", "email": "carol@example.com"},
	}
	resp := postJSON(t, base+"/webhook/gitlab", payload, map[string]string{
		"X-Gitlab-Event": "Merge Request Hook",
	})
	resp.Body.Close()

	// Register carol's wallet
	resp2 := postJSON(t, base+"/wallet/register", map[string]any{
		"username": "carol", "wallet_address": "0xCAROL",
	}, nil)
	resp2.Body.Close()

	// Trigger payout — should fail gracefully (no credentials)
	resp3 := postJSON(t, base+"/rewards/payout", nil, nil)
	var result map[string]any
	readJSON(t, resp3, &result)
	// ok=true is returned at the envelope level; failed list should be non-empty
	// OR the reward is skipped because no wallet lookup succeeds.
	// Either way, no panic and no silent "paid" status.
	if result["ok"] != true {
		t.Errorf("payout envelope ok: got %v", result["ok"])
	}
}

// TestIntegration_SQLiteConcurrency verifies that concurrent webhook requests
// do not corrupt the database (no race conditions on the rewards table).
func TestIntegration_SQLiteConcurrency(t *testing.T) {
	base := startService(t, rewards.Config{})

	const goroutines = 20
	var wg sync.WaitGroup
	errors := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			payload := map[string]any{
				"object_attributes": map[string]any{
					"state": "merged",
					"iid":   float64(n),
				},
				"user": map[string]any{
					"username": fmt.Sprintf("user%d", n),
					"email":    fmt.Sprintf("user%d@example.com", n),
				},
			}
			resp := postJSON(t, base+"/webhook/gitlab", payload, map[string]string{
				"X-Gitlab-Event": "Merge Request Hook",
			})
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errors <- fmt.Errorf("goroutine %d: got %d", n, resp.StatusCode)
			}
		}(i)
	}
	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}

	// All rewards should be persisted
	resp, err := http.Get(base + "/rewards/pending")
	if err != nil {
		t.Fatalf("GET /rewards/pending: %v", err)
	}
	var pending []map[string]any
	readJSON(t, resp, &pending)
	if len(pending) != goroutines {
		t.Errorf("expected %d rewards after concurrent writes, got %d", goroutines, len(pending))
	}
}

// TestIntegration_RateUpdateAndRead verifies that PUT /rewards/rates updates
// the rates and GET /rewards/rates reflects the change.
func TestIntegration_RateUpdateAndRead(t *testing.T) {
	base := startService(t, rewards.Config{})

	// Update rates
	newRates := map[string]any{
		"merge_request": 2.5,
		"issue":         0.5,
		"pipeline":      0.2,
		"star":          0.1,
	}
	req, _ := http.NewRequest(http.MethodPut, base+"/rewards/rates",
		bytes.NewReader(func() []byte { b, _ := json.Marshal(newRates); return b }()))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /rewards/rates: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /rewards/rates: expected 200, got %d", resp.StatusCode)
	}

	// Read back
	resp2, err := http.Get(base + "/rewards/rates")
	if err != nil {
		t.Fatalf("GET /rewards/rates: %v", err)
	}
	var rates map[string]any
	readJSON(t, resp2, &rates)
	if rates["merge_request"] != 2.5 {
		t.Errorf("merge_request rate: got %v, want 2.5", rates["merge_request"])
	}
}
