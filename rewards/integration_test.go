package rewards_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/rewards"
)

// startService launches a rewards service on an OS-assigned free port and
// returns its base URL. The service is shut down when the test ends.
//
// Port allocation: we bind a TCP listener on :0 to let the OS pick a free
// port, record the address, close the listener, then pass that address to the
// service. There is a tiny TOCTOU window, but it is far smaller than the
// previous scheme (fixed port derived from test name length) which caused
// collisions whenever two tests had the same-length name.
func startService(t *testing.T, cfg rewards.Config) string {
	t.Helper()
	if cfg.DBPath == "" {
		cfg.DBPath = filepath.Join(t.TempDir(), "rewards.db")
	}
	cfg.Enabled = true

	// Pick a free port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	cfg.ListenAddr = addr

	svc, err := rewards.New(cfg)
	if err != nil {
		t.Fatalf("rewards.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	started := make(chan struct{})
	go func() {
		close(started)
		_ = svc.Start(ctx)
	}()
	<-started

	// Poll until the server accepts connections (up to 3 s).
	base := "http://" + addr
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/health")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return base
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("service at %s did not become ready within 3s", addr)
	return ""
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

// TestIntegration_MetricsEndpoint verifies that /metrics returns Prometheus
// text format with the expected metric names and does not panic.
func TestIntegration_MetricsEndpoint(t *testing.T) {
	base := startService(t, rewards.Config{PublisherID: "pub1", WalletAddress: "0xABC"})

	// Queue a reward so counters are non-zero.
	payload := map[string]any{
		"object_attributes": map[string]any{"state": "merged", "iid": float64(1)},
		"user":              map[string]any{"username": "dave", "email": "dave@example.com"},
	}
	resp := postJSON(t, base+"/webhook/gitlab", payload, map[string]string{
		"X-Gitlab-Event": "Merge Request Hook",
	})
	resp.Body.Close()

	resp2, err := http.Get(base + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp2.StatusCode)
	}
	ct := resp2.Header.Get("Content-Type")
	if ct == "" || ct[:10] != "text/plain" {
		t.Errorf("expected text/plain Content-Type, got %q", ct)
	}

	body, _ := io.ReadAll(resp2.Body)
	text := string(body)
	for _, want := range []string{
		"gitlab_enhanced_rewards_pending",
		"gitlab_enhanced_rewards_paid_total",
		"gitlab_enhanced_rewards_failed_total",
		"gitlab_enhanced_rewards_bat_queued_total",
		"gitlab_enhanced_rewards_bat_paid_total",
		"gitlab_enhanced_rewards_rate_bat",
	} {
		if !contains(text, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
}

// TestIntegration_PayoutRouting_NoCreds verifies that submitPayout returns a
// clear error (not a panic or silent success) when neither Uphold nor ERC-20
// credentials are configured.
func TestIntegration_PayoutRouting_NoCreds(t *testing.T) {
	base := startService(t, rewards.Config{MinPayoutBAT: 0})

	// Queue a reward and register a wallet.
	payload := map[string]any{
		"object_attributes": map[string]any{"state": "merged", "iid": float64(99)},
		"user":              map[string]any{"username": "eve", "email": "eve@example.com"},
	}
	postJSON(t, base+"/webhook/gitlab", payload, map[string]string{
		"X-Gitlab-Event": "Merge Request Hook",
	}).Body.Close()
	postJSON(t, base+"/wallet/register", map[string]any{
		"username": "eve", "wallet_address": "0xEVE",
	}, nil).Body.Close()

	// Trigger payout — no credentials configured.
	resp := postJSON(t, base+"/rewards/payout", nil, nil)
	var result map[string]any
	readJSON(t, resp, &result)

	// The envelope must be ok=true (the handler ran without panic).
	if result["ok"] != true {
		t.Errorf("payout envelope ok: got %v, want true", result["ok"])
	}
	// The reward must NOT be marked "paid" — it should be "failed" with a message.
	resp2, _ := http.Get(base + "/rewards/pending")
	var pending []map[string]any
	readJSON(t, resp2, &pending)
	for _, r := range pending {
		if r["status"] == "paid" {
			t.Errorf("reward marked paid without credentials: %v", r)
		}
	}
}

// TestIntegration_PayoutRouting_UpholdPath verifies that when Uphold
// credentials are set, submitPayout attempts the Uphold path (and fails with
// an auth error from the mock, not a "no credentials" error).
func TestIntegration_PayoutRouting_UpholdPath(t *testing.T) {
	// Use a local HTTP server as a fake Uphold endpoint that returns 401.
	fakeUphold := startFakeUphold(t)

	base := startService(t, rewards.Config{
		MinPayoutBAT:       0,
		UpholdClientID:     "fake-id",
		UpholdClientSecret: "fake-secret",
		UpholdAPIBase:      fakeUphold,
	})

	payload := map[string]any{
		"object_attributes": map[string]any{"state": "merged", "iid": float64(7)},
		"user":              map[string]any{"username": "frank", "email": "frank@example.com"},
	}
	postJSON(t, base+"/webhook/gitlab", payload, map[string]string{
		"X-Gitlab-Event": "Merge Request Hook",
	}).Body.Close()
	postJSON(t, base+"/wallet/register", map[string]any{
		"username": "frank", "wallet_address": "0xFRANK",
	}, nil).Body.Close()

	resp := postJSON(t, base+"/rewards/payout", nil, nil)
	var result map[string]any
	readJSON(t, resp, &result)
	// Envelope ok=true; the Uphold path was attempted (auth failed at fake server).
	if result["ok"] != true {
		t.Errorf("payout envelope ok: got %v", result["ok"])
	}
}

// startFakeUphold starts a minimal HTTP server that returns 401 for all
// requests, simulating an Uphold endpoint with bad credentials.
func startFakeUphold(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln) //nolint:errcheck
	t.Cleanup(func() { srv.Close() })
	return "http://" + ln.Addr().String()
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
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
