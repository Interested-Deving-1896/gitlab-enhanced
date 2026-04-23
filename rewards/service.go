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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
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
	ID          string    `json:"id"`
	Event       ContributionEvent `json:"event"`
	AmountBAT   float64   `json:"amount_bat"`
	QueuedAt    time.Time `json:"queued_at"`
	PaidAt      *time.Time `json:"paid_at,omitempty"`
	TxHash      string    `json:"tx_hash,omitempty"`
	Status      string    `json:"status"` // pending | paid | failed
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
	cfg      Config
	rates    RewardRates
	mu       sync.RWMutex
	pending  []PendingReward
	wallets  map[string]WalletRegistration // keyed by username
	server   *http.Server
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
	s := &Service{
		cfg:     cfg,
		rates:   DefaultRewardRates(),
		wallets: make(map[string]WalletRegistration),
	}
	return s, nil
}

// Start launches the HTTP server. Blocks until ctx is cancelled.
func (s *Service) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
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
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("[rewards] service starting on http://%s", s.cfg.ListenAddr)
	log.Printf("[rewards] publisher ID: %s", s.cfg.PublisherID)
	log.Printf("[rewards] wallet: %s", s.cfg.WalletAddress)

	errCh := make(chan error, 1)
	go func() { errCh <- s.server.ListenAndServe() }()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.server.Shutdown(shutCtx)
	case err := <-errCh:
		return err
	}
}

// QueueReward adds a pending reward for a contribution event.
func (s *Service) QueueReward(event ContributionEvent) PendingReward {
	amount := s.rateFor(event.Type)
	reward := PendingReward{
		ID:        fmt.Sprintf("%s-%d-%d", event.Type, event.ProjectID, event.ObjectID),
		Event:     event,
		AmountBAT: amount,
		QueuedAt:  time.Now().UTC(),
		Status:    "pending",
	}
	s.mu.Lock()
	s.pending = append(s.pending, reward)
	s.mu.Unlock()
	log.Printf("[rewards] queued %.3f BAT for %s (%s #%d)",
		amount, event.ContributorUsername, event.Type, event.ObjectID)
	return reward
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

// handleGitLabWebhook receives GitLab system hooks and queues rewards.
// Configure in GitLab Admin > System Hooks with the URL of this endpoint.
func (s *Service) handleGitLabWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	// Parse the GitLab webhook event type from the header
	eventType := r.Header.Get("X-Gitlab-Event")

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	var event *ContributionEvent

	// safeAttrs extracts object_attributes as map[string]any, returning nil if absent.
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
		v, _ := m[key].(float64) // JSON numbers decode as float64
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
			event = &ContributionEvent{
				Type:                "pipeline",
				ContributorUsername: strField(user, "username"),
				ContributorEmail:    strField(user, "email"),
				ObjectID:            intField(attrs, "id"),
			}
		}
	}

	if event == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "action": "ignored"})
		return
	}

	reward := s.QueueReward(*event)
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
	s.wallets[reg.Username] = reg
	s.mu.Unlock()
	log.Printf("[rewards] registered wallet for %s: %s", reg.Username, reg.WalletAddress)
	writeJSON(w, http.StatusOK, reg)
}

func (s *Service) handleWalletGet(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Path[len("/wallet/"):]
	s.mu.RLock()
	reg, ok := s.wallets[username]
	s.mu.RUnlock()
	if !ok {
		http.Error(w, "wallet not registered", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, reg)
}

func (s *Service) handlePendingRewards(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	pending := make([]PendingReward, len(s.pending))
	copy(pending, s.pending)
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, pending)
}

func (s *Service) handleRates(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, s.rates)
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
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, s.rates)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// handlePayout triggers payout of all pending rewards above MinPayoutBAT.
// In production this would call the Uphold API or submit an ERC-20 transaction.
// Currently logs the payout intent and marks rewards as paid.
func (s *Service) handlePayout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var totalBAT float64
	var paid []string
	now := time.Now().UTC()

	for i := range s.pending {
		if s.pending[i].Status != "pending" {
			continue
		}
		totalBAT += s.pending[i].AmountBAT
		if totalBAT >= s.cfg.MinPayoutBAT {
			// Look up the contributor's wallet
			wallet, ok := s.wallets[s.pending[i].Event.ContributorUsername]
			if !ok {
				log.Printf("[rewards] no wallet registered for %s — skipping payout",
					s.pending[i].Event.ContributorUsername)
				continue
			}
			// TODO: submit ERC-20 transfer via go-ethereum or Uphold API
			log.Printf("[rewards] PAYOUT %.3f BAT → %s (%s)",
				s.pending[i].AmountBAT, wallet.WalletAddress,
				s.pending[i].Event.ContributorUsername)
			s.pending[i].Status = "paid"
			s.pending[i].PaidAt = &now
			s.pending[i].TxHash = "pending-implementation"
			paid = append(paid, s.pending[i].ID)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"paid":      paid,
		"total_bat": totalBAT,
		"note":      "on-chain transfer not yet implemented — logged only",
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
