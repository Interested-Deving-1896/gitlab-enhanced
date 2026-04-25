// Package gitlab implements the GitLab CI webhook listener and runner
// registration API client for garm-gitlab.
//
// GitLab CI does not use JIT tokens like GitHub Actions. Instead it uses a
// persistent runner registration token model:
//
//  1. A runner is registered via POST /api/v4/runners → returns a runner token.
//  2. gitlab-runner polls for jobs using that token.
//  3. When a job is queued, GitLab sends a webhook (job event) if configured.
//
// garm-gitlab listens for job webhooks to know when to scale up, and polls
// the GitLab API to know when runners are idle so it can scale down.
package gitlab

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
)

// JobEvent is the payload GitLab sends for job hooks.
// Ref: https://docs.gitlab.com/ee/user/project/integrations/webhook_events.html#job-events
type JobEvent struct {
	ObjectKind        string    `json:"object_kind"`
	Ref               string    `json:"ref"`
	Tag               bool      `json:"tag"`
	BeforeSHA         string    `json:"before_sha"`
	SHA               string    `json:"sha"`
	BuildID           int64     `json:"build_id"`
	BuildName         string    `json:"build_name"`
	BuildStage        string    `json:"build_stage"`
	BuildStatus       string    `json:"build_status"`
	BuildCreatedAt    time.Time `json:"build_created_at"`
	BuildStartedAt    time.Time `json:"build_started_at"`
	BuildFinishedAt   time.Time `json:"build_finished_at"`
	BuildDuration     float64   `json:"build_duration"`
	BuildAllowFailure bool      `json:"build_allow_failure"`
	BuildFailureReason string   `json:"build_failure_reason"`
	PipelineID        int64     `json:"pipeline_id"`
	RunnerID          int64     `json:"runner_id"`
	RunnerDescription string    `json:"runner_description"`
	RunnerTags        []string  `json:"runner_tags"`
	ProjectID         int64     `json:"project_id"`
	ProjectName       string    `json:"project_name"`
	User              struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"user"`
	Commit struct {
		ID      int64  `json:"id"`
		SHA     string `json:"sha"`
		Message string `json:"message"`
	} `json:"commit"`
	Repository struct {
		Name            string `json:"name"`
		HTTPURL         string `json:"git_http_url"`
		SSHURL          string `json:"git_ssh_url"`
		VisibilityLevel int    `json:"visibility_level"`
	} `json:"repository"`
}

// JobStatus values from GitLab.
const (
	JobStatusCreated  = "created"
	JobStatusPending  = "pending"
	JobStatusRunning  = "running"
	JobStatusFailed   = "failed"
	JobStatusSuccess  = "success"
	JobStatusCanceled = "canceled"
)

// WebhookHandler receives GitLab job webhook events and dispatches them to
// the pool manager via the provided channel.
type WebhookHandler struct {
	secret  string
	eventCh chan<- JobEvent
	log     *logrus.Logger
}

// NewWebhookHandler creates a WebhookHandler. secret is the GitLab webhook
// secret token configured on the project/group webhook. eventCh receives
// every validated job event.
func NewWebhookHandler(secret string, eventCh chan<- JobEvent, log *logrus.Logger) *WebhookHandler {
	return &WebhookHandler{
		secret:  secret,
		eventCh: eventCh,
		log:     log,
	}
}

// ServeHTTP implements http.Handler. It validates the X-Gitlab-Token header,
// parses the job event, and forwards it to the pool manager.
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate webhook token.
	if err := h.validateToken(r); err != nil {
		h.log.WithError(err).Warn("webhook token validation failed")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Only handle job events.
	eventType := r.Header.Get("X-Gitlab-Event")
	if eventType != "Job Hook" {
		w.WriteHeader(http.StatusOK)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB limit
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var event JobEvent
	if err := json.Unmarshal(body, &event); err != nil {
		h.log.WithError(err).Error("failed to parse job event")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	h.log.WithFields(logrus.Fields{
		"build_id":     event.BuildID,
		"build_status": event.BuildStatus,
		"project":      event.ProjectName,
		"tags":         strings.Join(event.RunnerTags, ","),
	}).Debug("received job event")

	// Non-blocking send — drop if pool manager is busy.
	select {
	case h.eventCh <- event:
	default:
		h.log.Warn("event channel full, dropping job event")
	}

	w.WriteHeader(http.StatusOK)
}

// validateToken checks the X-Gitlab-Token header against the configured secret.
// GitLab sends the raw secret as a header value (not HMAC-signed).
// For HMAC validation (future), use validateHMAC instead.
func (h *WebhookHandler) validateToken(r *http.Request) error {
	if h.secret == "" {
		return nil // no secret configured — accept all (not recommended for production)
	}
	token := r.Header.Get("X-Gitlab-Token")
	if token == "" {
		return fmt.Errorf("missing X-Gitlab-Token header")
	}
	if !hmac.Equal([]byte(token), []byte(h.secret)) {
		return fmt.Errorf("token mismatch")
	}
	return nil
}

// validateHMAC validates an HMAC-SHA256 signature (for future GitLab versions
// that may adopt signed webhooks similar to GitHub).
func validateHMAC(secret, signature string, body []byte) error {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return fmt.Errorf("HMAC signature mismatch")
	}
	return nil
}

// WebhookListener wraps WebhookHandler in an HTTP server.
type WebhookListener struct {
	srv *http.Server
	log *logrus.Logger
}

// NewWebhookListener creates a WebhookListener bound to addr. secret and
// eventCh are forwarded to the underlying WebhookHandler.
func NewWebhookListener(addr, secret string, eventCh chan<- JobEvent, log *logrus.Logger) *WebhookListener {
	handler := NewWebhookHandler(secret, eventCh, log)

	r := mux.NewRouter()
	r.Handle("/webhook", handler).Methods(http.MethodPost)
	r.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}).Methods(http.MethodGet)

	return &WebhookListener{
		srv: &http.Server{
			Addr:    addr,
			Handler: r,
		},
		log: log,
	}
}

// ListenAndServe starts the HTTP server and blocks until ctx is cancelled,
// at which point it performs a graceful shutdown.
func (l *WebhookListener) ListenAndServe(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		if err := l.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		l.log.Info("shutting down webhook listener")
		return l.srv.Shutdown(context.Background())
	}
}
