// Package rewards implements the opt-in BAT (Basic Attention Token) rewards
// service for gitlab-enhanced.
//
// This service is DISABLED by default. It activates only when
// config.rewards.enabled = true is explicitly set in config/local.yaml.
//
// Architecture:
//   - No brave-core dependency. BAT is an ERC-20 token; all interactions use
//     the Brave publisher verification API and standard Ethereum JSON-RPC.
//   - Custodial path: Uphold REST API for fiat-equivalent payouts.
//   - Non-custodial path: direct ERC-20 transfers via go-ethereum (planned).
//
// Reward triggers:
//   - Merged MR:        contributor receives a configurable BAT tip
//   - Closed issue:     smaller tip for issue reporters
//   - CI pipeline pass: tip for the pipeline author (opt-in per project)
//   - Repository star:  micro-tip for the repository maintainer
//
// The service exposes a JSON HTTP API consumed by GitLab webhooks.
package rewards

import (
	"bytes"
	"context"
	"crypto/hmac"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/store"
)

// Config holds rewards service configuration. Populated from config.Rewards.
type Config struct {
	Enabled            bool
	PublisherID        string
	WalletAddress      string
	UpholdClientID     string
	UpholdClientSecret string
	MinPayoutBAT       float64
	ListenAddr         string
	// WebhookSecret is the token set in GitLab Admin > System Hooks.
	// When non-empty every incoming webhook is validated against the
	// X-Gitlab-Token header; requests with a wrong or missing token are
	// rejected with 401.
	WebhookSecret string
	// DBPath is the path to the SQLite database file.
	// Defaults to /var/lib/gitlab-enhanced/rewards.db
	DBPath string
	// BandwidthAddr is the address of the bandwidth service HTTP API.
	// When set, successful Pipeline Hook events trigger artifact registration
	// via POST /artifacts/register on the bandwidth service.
	// Example: "127.0.0.1:6062"
	BandwidthAddr string
}

// ContributionEvent describes a GitLab event that triggers a BAT reward.
type ContributionEvent struct {
	// Type is the event kind: "merge_request", "issue", "pipeline", "star"
	Type string `json:"type"`
	// ProjectID is the GitLab project ID.
	ProjectID int `json:"project_id"`
	// ProjectPath is the full project path (e.g. "mygroup/myrepo").
	ProjectPath string `json:"project_path"`
	// ContributorUsername is the GitLab username of the contributor to reward.
	ContributorUsername string `json:"contributor_username"`
	// ContributorEmail is used to look up the contributor's registered wallet.
	ContributorEmail string `json:"contributor_email"`
	// ObjectID is the MR/issue/pipeline ID.
	ObjectID int `json:"object_id"`
}

// PendingReward is a reward queued for payout.
type PendingReward struct {
	ID        string            `json:"id"`
	Event     ContributionEvent `json:"event"`
	AmountBAT float64           `json:"amount_bat"`
	QueuedAt  time.Time         `json:"queued_at"`
	PaidAt    *time.Time        `json:"paid_at,omitempty"`
	TxHash    string            `json:"tx_hash,omitempty"`
	Status    string            `json:"status"` // pending | paid | failed
}

// WalletRegistration maps a GitLab username to a BAT wallet address.
type WalletRegistration struct {
	Username      string    `json:"username"`
	WalletAddress string    `json:"wallet_address"`
	RegisteredAt  time.Time `json:"registered_at"`
	// UpholdCardID is set when the user connects an Uphold account.
	UpholdCardID string `json:"uphold_card_id,omitempty"`
}

// RewardRates defines how many BAT each event type earns.
type RewardRates struct {
	MergeRequest float64 `json:"merge_request"` // default: 1.0 BAT
	Issue        float64 `json:"issue"`         // default: 0.25 BAT
	Pipeline     float64 `json:"pipeline"`      // default: 0.1 BAT
	Star         float64 `json:"star"`          // default: 0.05 BAT
}

// DefaultRewardRates returns conservative default rates.
func DefaultRewardRates() RewardRates {
	return RewardRates{
		MergeRequest: 1.0,
		Issue:        0.25,
		Pipeline:     0.1,
		Star:         0.05,
	}
}

// Service is the rewards HTTP service.
type Service struct {
	cfg    Config
	rates  RewardRates
	mu     sync.RWMutex
	db     *sql.DB
	server *http.Server
}

// New creates a new rewards Service. Returns an error if rewards are disabled.
func New(cfg Config) (*Service, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("rewards service is disabled (set rewards.enabled: true in config/local.yaml)")
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:6061"
	}
	if cfg.MinPayoutBAT == 0 {
		cfg.MinPayoutBAT = 5.0
	}
	if cfg.DBPath == "" {
		cfg.DBPath = "/var/lib/gitlab-enhanced/rewards.db"
	}

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("rewards: open store: %w", err)
	}

	svc := &Service{
		cfg: cfg,
		db:  db,
	}
	// Load persisted rates; fall back to defaults if none saved yet.
	svc.rates = svc.loadRates()
	return svc, nil
}

// Start launches the HTTP server. Blocks until ctx is cancelled.
func (s *Service) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/webhook/gitlab", s.handleGitLabWebhook)
	mux.HandleFunc("/wallet/register", s.handleWalletRegister)
	mux.HandleFunc("/wallet/", s.handleWalletGet)
	mux.HandleFunc("/rewards/pending", s.handlePendingRewards)
	mux.HandleFunc("/rewards/rates", s.handleRates)
	mux.HandleFunc("/rewards/payout", s.handlePayout)

	s.server = &http.Server{
		Addr:         s.cfg.ListenAddr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	log.Printf("[rewards] service starting on http://%s", s.cfg.ListenAddr)
	log.Printf("[rewards] publisher ID: %s", s.cfg.PublisherID)
	log.Printf("[rewards] wallet: %s", s.cfg.WalletAddress)
	if s.cfg.WebhookSecret != "" {
		log.Printf("[rewards] webhook token validation enabled")
	} else {
		log.Printf("[rewards] WARNING: webhook_secret not set — all webhook requests accepted")
	}

	errCh := make(chan error, 1)
	go func() { errCh <- s.server.ListenAndServe() }()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutErr := s.server.Shutdown(shutCtx)
		s.db.Close()
		return shutErr
	case err := <-errCh:
		s.db.Close()
		return err
	}
}

// QueueReward adds a pending reward for a contribution event and persists it.
func (s *Service) QueueReward(event ContributionEvent) (PendingReward, error) {
	amount := s.rateFor(event.Type)
	reward := PendingReward{
		ID:        fmt.Sprintf("%s-%d-%d", event.Type, event.ProjectID, event.ObjectID),
		Event:     event,
		AmountBAT: amount,
		QueuedAt:  time.Now().UTC(),
		Status:    "pending",
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO rewards
			(id, event_type, project_id, project_path, contributor_username,
			 contributor_email, object_id, amount_bat, status, queued_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?)`,
		reward.ID, event.Type, event.ProjectID, event.ProjectPath,
		event.ContributorUsername, event.ContributorEmail, event.ObjectID,
		reward.AmountBAT, reward.QueuedAt.Format(time.RFC3339),
	)
	if err != nil {
		return PendingReward{}, fmt.Errorf("persist reward: %w", err)
	}

	log.Printf("[rewards] queued %.3f BAT for %s (%s #%d)",
		amount, event.ContributorUsername, event.Type, event.ObjectID)
	return reward, nil
}

func (s *Service) rateFor(eventType string) float64 {
	switch eventType {
	case "merge_request":
		return s.rates.MergeRequest
	case "issue":
		return s.rates.Issue
	case "pipeline":
		return s.rates.Pipeline
	case "star":
		return s.rates.Star
	default:
		return 0
	}
}

// --- HTTP handlers ---

func (s *Service) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"publisher_id": s.cfg.PublisherID,
		"wallet":       s.cfg.WalletAddress,
	})
}

// handleMetrics emits Prometheus text format metrics for scraping.
func (s *Service) handleMetrics(w http.ResponseWriter, r *http.Request) {
	pending, _ := s.loadPendingRewards()
	var totalPending, totalPaid, totalFailed int
	var batQueued, batPaid float64
	for _, rw := range pending {
		switch rw.Status {
		case "pending":
			totalPending++
			batQueued += rw.AmountBAT
		case "paid":
			totalPaid++
			batPaid += rw.AmountBAT
		case "failed":
			totalFailed++
		}
	}

	s.mu.RLock()
	rates := s.rates
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP gitlab_enhanced_rewards_pending Total rewards awaiting payout\n")
	fmt.Fprintf(w, "# TYPE gitlab_enhanced_rewards_pending gauge\n")
	fmt.Fprintf(w, "gitlab_enhanced_rewards_pending %d\n", totalPending)
	fmt.Fprintf(w, "# HELP gitlab_enhanced_rewards_paid_total Total rewards successfully paid\n")
	fmt.Fprintf(w, "# TYPE gitlab_enhanced_rewards_paid_total counter\n")
	fmt.Fprintf(w, "gitlab_enhanced_rewards_paid_total %d\n", totalPaid)
	fmt.Fprintf(w, "# HELP gitlab_enhanced_rewards_failed_total Total rewards that failed payout\n")
	fmt.Fprintf(w, "# TYPE gitlab_enhanced_rewards_failed_total counter\n")
	fmt.Fprintf(w, "gitlab_enhanced_rewards_failed_total %d\n", totalFailed)
	fmt.Fprintf(w, "# HELP gitlab_enhanced_rewards_bat_queued_total BAT queued for payout\n")
	fmt.Fprintf(w, "# TYPE gitlab_enhanced_rewards_bat_queued_total gauge\n")
	fmt.Fprintf(w, "gitlab_enhanced_rewards_bat_queued_total %.8f\n", batQueued)
	fmt.Fprintf(w, "# HELP gitlab_enhanced_rewards_bat_paid_total BAT successfully paid out\n")
	fmt.Fprintf(w, "# TYPE gitlab_enhanced_rewards_bat_paid_total counter\n")
	fmt.Fprintf(w, "gitlab_enhanced_rewards_bat_paid_total %.8f\n", batPaid)
	fmt.Fprintf(w, "# HELP gitlab_enhanced_rewards_rate_bat Rate in BAT per event type\n")
	fmt.Fprintf(w, "# TYPE gitlab_enhanced_rewards_rate_bat gauge\n")
	fmt.Fprintf(w, "gitlab_enhanced_rewards_rate_bat{event=\"merge_request\"} %.4f\n", rates.MergeRequest)
	fmt.Fprintf(w, "gitlab_enhanced_rewards_rate_bat{event=\"issue\"} %.4f\n", rates.Issue)
	fmt.Fprintf(w, "gitlab_enhanced_rewards_rate_bat{event=\"pipeline\"} %.4f\n", rates.Pipeline)
	fmt.Fprintf(w, "gitlab_enhanced_rewards_rate_bat{event=\"star\"} %.4f\n", rates.Star)
}

// handleGitLabWebhook receives GitLab system hooks and queues rewards.
// Configure in GitLab Admin > System Hooks with the URL of this endpoint.
// Set the secret token in GitLab and mirror it in rewards.webhook_secret.
func (s *Service) handleGitLabWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate the GitLab webhook secret when configured.
	// GitLab sends the secret verbatim in X-Gitlab-Token (not as an HMAC).
	if s.cfg.WebhookSecret != "" {
		token := r.Header.Get("X-Gitlab-Token")
		if !hmac.Equal([]byte(token), []byte(s.cfg.WebhookSecret)) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			log.Printf("[rewards] webhook rejected: invalid X-Gitlab-Token from %s", r.RemoteAddr)
			return
		}
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	eventType := r.Header.Get("X-Gitlab-Event")

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	var event *ContributionEvent

	safeAttrs := func(p map[string]any) map[string]any {
		v, _ := p["object_attributes"].(map[string]any)
		return v
	}
	safeUser := func(p map[string]any) map[string]any {
		v, _ := p["user"].(map[string]any)
		return v
	}
	strField := func(m map[string]any, key string) string {
		if m == nil {
			return ""
		}
		v, _ := m[key].(string)
		return v
	}
	intField := func(m map[string]any, key string) int {
		if m == nil {
			return 0
		}
		v, _ := m[key].(float64)
		return int(v)
	}

	switch eventType {
	case "Merge Request Hook":
		attrs := safeAttrs(payload)
		if strField(attrs, "state") == "merged" {
			user := safeUser(payload)
			event = &ContributionEvent{
				Type:                "merge_request",
				ContributorUsername: strField(user, "username"),
				ContributorEmail:    strField(user, "email"),
				ObjectID:            intField(attrs, "iid"),
			}
		}
	case "Issue Hook":
		attrs := safeAttrs(payload)
		if strField(attrs, "action") == "close" {
			user := safeUser(payload)
			event = &ContributionEvent{
				Type:                "issue",
				ContributorUsername: strField(user, "username"),
				ContributorEmail:    strField(user, "email"),
				ObjectID:            intField(attrs, "iid"),
			}
		}
	case "Pipeline Hook":
		attrs := safeAttrs(payload)
		if strField(attrs, "status") == "success" {
			user := safeUser(payload)
			pipelineID := intField(attrs, "id")
			event = &ContributionEvent{
				Type:                "pipeline",
				ContributorUsername: strField(user, "username"),
				ContributorEmail:    strField(user, "email"),
				ObjectID:            pipelineID,
			}
			// Auto-register pipeline artifacts with the bandwidth service so
			// the retention enforcer can manage them.
			if s.cfg.BandwidthAddr != "" {
				projectID := intField(payload, "project_id")
				go s.registerPipelineArtifacts(projectID, pipelineID, payload)
			}
		}
	}

	if event == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "action": "ignored"})
		return
	}

	reward, err := s.QueueReward(*event)
	if err != nil {
		http.Error(w, "failed to queue reward", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "reward_id": reward.ID, "amount_bat": reward.AmountBAT})
}

func (s *Service) handleWalletRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var reg WalletRegistration
	if err := json.NewDecoder(r.Body).Decode(&reg); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if reg.Username == "" || reg.WalletAddress == "" {
		http.Error(w, "username and wallet_address required", http.StatusBadRequest)
		return
	}
	reg.RegisteredAt = time.Now().UTC()

	s.mu.Lock()
	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO wallets (username, wallet_address, uphold_card_id, registered_at)
		VALUES (?, ?, ?, ?)`,
		reg.Username, reg.WalletAddress, reg.UpholdCardID,
		reg.RegisteredAt.Format(time.RFC3339),
	)
	s.mu.Unlock()

	if err != nil {
		http.Error(w, "failed to register wallet", http.StatusInternalServerError)
		return
	}
	log.Printf("[rewards] registered wallet for %s: %s", reg.Username, reg.WalletAddress)
	writeJSON(w, http.StatusOK, reg)
}

func (s *Service) handleWalletGet(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Path[len("/wallet/"):]
	reg, err := s.loadWallet(username)
	if err == sql.ErrNoRows {
		http.Error(w, "wallet not registered", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, reg)
}

func (s *Service) handlePendingRewards(w http.ResponseWriter, r *http.Request) {
	pending, err := s.loadPendingRewards()
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, pending)
}

func (s *Service) handleRates(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.mu.RLock()
		rates := s.rates
		s.mu.RUnlock()
		writeJSON(w, http.StatusOK, rates)
		return
	}
	if r.Method == http.MethodPut {
		var rates RewardRates
		if err := json.NewDecoder(r.Body).Decode(&rates); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.rates = rates
		saveErr := s.saveRates(rates)
		s.mu.Unlock()
		if saveErr != nil {
			log.Printf("[rewards] failed to persist rates: %v", saveErr)
		}
		writeJSON(w, http.StatusOK, rates)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// handlePayout triggers payout of all pending rewards above MinPayoutBAT.
// When UpholdClientID/Secret are configured, payouts go through the Uphold
// REST API. Without credentials the handler returns an error rather than
// silently marking rewards as paid.
func (s *Service) handlePayout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pending, err := s.loadPendingRewards()
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	var paid []string
	var failed []string
	var totalBAT float64

	for _, reward := range pending {
		if reward.Status != "pending" {
			continue
		}
		totalBAT += reward.AmountBAT
		if totalBAT < s.cfg.MinPayoutBAT {
			continue
		}

		wallet, walletErr := s.loadWallet(reward.Event.ContributorUsername)
		if walletErr == sql.ErrNoRows {
			log.Printf("[rewards] no wallet registered for %s — skipping payout",
				reward.Event.ContributorUsername)
			continue
		}
		if walletErr != nil {
			log.Printf("[rewards] wallet lookup error for %s: %v",
				reward.Event.ContributorUsername, walletErr)
			continue
		}

		txHash, payErr := s.submitPayout(reward, wallet)
		now := time.Now().UTC()
		if payErr != nil {
			log.Printf("[rewards] payout failed for %s: %v", reward.ID, payErr)
			s.mu.Lock()
			_, _ = s.db.Exec(
				`UPDATE rewards SET status='failed', paid_at=? WHERE id=?`,
				now.Format(time.RFC3339), reward.ID,
			)
			s.mu.Unlock()
			failed = append(failed, reward.ID)
			continue
		}

		s.mu.Lock()
		_, _ = s.db.Exec(
			`UPDATE rewards SET status='paid', paid_at=?, tx_hash=? WHERE id=?`,
			now.Format(time.RFC3339), txHash, reward.ID,
		)
		s.mu.Unlock()
		log.Printf("[rewards] paid %.3f BAT → %s (%s) tx=%s",
			reward.AmountBAT, wallet.WalletAddress, reward.Event.ContributorUsername, txHash)
		paid = append(paid, reward.ID)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"paid":      paid,
		"failed":    failed,
		"total_bat": totalBAT,
	})
}

// registerPipelineArtifacts extracts artifact paths from a Pipeline Hook
// payload and registers each one with the bandwidth service for retention
// tracking. Runs in a goroutine — failures are logged, not fatal.
func (s *Service) registerPipelineArtifacts(projectID, pipelineID int, payload map[string]any) {
	builds, _ := payload["builds"].([]any)
	for _, b := range builds {
		build, ok := b.(map[string]any)
		if !ok {
			continue
		}
		artifacts, _ := build["artifacts"].([]any)
		for _, af := range artifacts {
			artifact, ok := af.(map[string]any)
			if !ok {
				continue
			}
			path, _ := artifact["filename"].(string)
			if path == "" {
				continue
			}
			size, _ := artifact["size"].(float64)
			jobID := int(func() float64 { v, _ := build["id"].(float64); return v }())

			body, _ := json.Marshal(map[string]any{
				"path":       path,
				"size_bytes": int64(size),
				"project_id": projectID,
				"job_id":     jobID,
				"created_at": time.Now().UTC().Format(time.RFC3339),
			})
			resp, err := http.Post(
				"http://"+s.cfg.BandwidthAddr+"/artifacts/register",
				"application/json",
				bytes.NewReader(body),
			)
			if err != nil {
				log.Printf("[rewards] artifact register: %v", err)
				continue
			}
			resp.Body.Close()
			if resp.StatusCode >= 300 {
				log.Printf("[rewards] artifact register: bandwidth returned %d for %s", resp.StatusCode, path)
			}
		}
	}
}

// submitPayout sends BAT to the contributor's wallet via the Uphold REST API.
// Returns the Uphold transaction ID on success.
func (s *Service) submitPayout(reward PendingReward, wallet WalletRegistration) (string, error) {
	if s.cfg.UpholdClientID == "" || s.cfg.UpholdClientSecret == "" {
		return "", fmt.Errorf("uphold credentials not configured (set rewards.uphold_client_id and rewards.uphold_client_secret)")
	}

	// Step 1: obtain a short-lived OAuth2 bearer token.
	token, err := s.upholdToken()
	if err != nil {
		return "", fmt.Errorf("uphold auth: %w", err)
	}

	// Step 2: create and commit a transaction from the publisher card.
	// For users with an Uphold account we use their card ID for an internal
	// transfer; for external ERC-20 addresses we send to the wallet address.
	destination := wallet.WalletAddress
	if wallet.UpholdCardID != "" {
		destination = wallet.UpholdCardID
	}

	txBody, _ := json.Marshal(map[string]any{
		"denomination": map[string]any{
			"amount":   fmt.Sprintf("%.8f", reward.AmountBAT),
			"currency": "BAT",
		},
		"destination": destination,
	})

	req, err := http.NewRequest(http.MethodPost,
		"https://api.uphold.com/v0/me/cards/"+s.cfg.PublisherID+"/transactions?commit=true",
		bytes.NewReader(txBody),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("uphold transaction: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("uphold response decode: %w", err)
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("uphold API error %d: %v", resp.StatusCode, result)
	}

	txID, _ := result["id"].(string)
	if txID == "" {
		return "", fmt.Errorf("uphold returned no transaction ID")
	}
	return txID, nil
}

// upholdToken fetches a short-lived OAuth2 bearer token from Uphold.
func (s *Service) upholdToken() (string, error) {
	body := []byte("grant_type=client_credentials")
	req, err := http.NewRequest(http.MethodPost,
		"https://api.uphold.com/oauth2/token",
		bytes.NewReader(body),
	)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(s.cfg.UpholdClientID, s.cfg.UpholdClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("uphold token request: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("uphold token decode: %w", err)
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("uphold token error %d: %v", resp.StatusCode, result)
	}
	token, _ := result["access_token"].(string)
	if token == "" {
		return "", fmt.Errorf("uphold returned no access_token")
	}
	return token, nil
}

// --- DB helpers ---

func (s *Service) loadWallet(username string) (WalletRegistration, error) {
	var reg WalletRegistration
	var registeredAt string
	err := s.db.QueryRow(
		`SELECT username, wallet_address, uphold_card_id, registered_at FROM wallets WHERE username=?`,
		username,
	).Scan(&reg.Username, &reg.WalletAddress, &reg.UpholdCardID, &registeredAt)
	if err != nil {
		return WalletRegistration{}, err
	}
	reg.RegisteredAt, _ = time.Parse(time.RFC3339, registeredAt)
	return reg, nil
}

func (s *Service) loadPendingRewards() ([]PendingReward, error) {
	rows, err := s.db.Query(`
		SELECT id, event_type, project_id, project_path, contributor_username,
		       contributor_email, object_id, amount_bat, status, queued_at, paid_at, tx_hash
		FROM rewards ORDER BY queued_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PendingReward
	for rows.Next() {
		var r PendingReward
		var queuedAt string
		var paidAt sql.NullString
		err := rows.Scan(
			&r.ID, &r.Event.Type, &r.Event.ProjectID, &r.Event.ProjectPath,
			&r.Event.ContributorUsername, &r.Event.ContributorEmail, &r.Event.ObjectID,
			&r.AmountBAT, &r.Status, &queuedAt, &paidAt, &r.TxHash,
		)
		if err != nil {
			return nil, err
		}
		r.QueuedAt, _ = time.Parse(time.RFC3339, queuedAt)
		if paidAt.Valid {
			t, _ := time.Parse(time.RFC3339, paidAt.String)
			r.PaidAt = &t
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// loadRates reads persisted reward rates from the settings table.
// Returns DefaultRewardRates() if no rates have been saved yet.
func (s *Service) loadRates() RewardRates {
	var raw string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key='reward_rates'`).Scan(&raw)
	if err != nil {
		return DefaultRewardRates()
	}
	var rates RewardRates
	if err := json.Unmarshal([]byte(raw), &rates); err != nil {
		return DefaultRewardRates()
	}
	return rates
}

// saveRates persists the current reward rates to the settings table.
func (s *Service) saveRates(rates RewardRates) error {
	raw, err := json.Marshal(rates)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT OR REPLACE INTO settings (key, value) VALUES ('reward_rates', ?)`,
		string(raw),
	)
	return err
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
