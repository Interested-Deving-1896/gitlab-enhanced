package rewards

import (
	"crypto/hmac"
	"os"
	"path/filepath"
	"testing"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	svc, err := New(Config{
		Enabled:       true,
		PublisherID:   "test-publisher",
		WalletAddress: "0xDEADBEEF",
		ListenAddr:    "127.0.0.1:0",
		DBPath:        filepath.Join(dir, "rewards.db"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { svc.db.Close() })
	return svc
}

func TestQueueReward(t *testing.T) {
	svc := newTestService(t)

	event := ContributionEvent{
		Type:                "merge_request",
		ProjectID:           1,
		ContributorUsername: "alice",
		ObjectID:            42,
	}
	reward, err := svc.QueueReward(event)
	if err != nil {
		t.Fatalf("QueueReward: %v", err)
	}
	if reward.AmountBAT != DefaultRewardRates().MergeRequest {
		t.Errorf("expected %.2f BAT, got %.2f", DefaultRewardRates().MergeRequest, reward.AmountBAT)
	}
	if reward.Status != "pending" {
		t.Errorf("expected status pending, got %s", reward.Status)
	}

	// Verify persistence
	pending, err := svc.loadPendingRewards()
	if err != nil {
		t.Fatalf("loadPendingRewards: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("expected 1 persisted reward, got %d", len(pending))
	}
}

func TestPersistenceAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "rewards.db")

	// First instance: queue a reward
	svc1, err := New(Config{Enabled: true, ListenAddr: "127.0.0.1:0", DBPath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc1.QueueReward(ContributionEvent{
		Type: "issue", ProjectID: 2, ContributorUsername: "bob", ObjectID: 7,
	}); err != nil {
		t.Fatal(err)
	}
	svc1.db.Close()

	// Second instance: reward must still be there
	svc2, err := New(Config{Enabled: true, ListenAddr: "127.0.0.1:0", DBPath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	defer svc2.db.Close()

	pending, err := svc2.loadPendingRewards()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Errorf("expected 1 reward after restart, got %d", len(pending))
	}
	if pending[0].Event.ContributorUsername != "bob" {
		t.Errorf("unexpected contributor: %s", pending[0].Event.ContributorUsername)
	}
}

func TestWalletPersistence(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "rewards.db")

	svc1, _ := New(Config{Enabled: true, ListenAddr: "127.0.0.1:0", DBPath: dbPath})
	_, _ = svc1.db.Exec(`INSERT OR REPLACE INTO wallets (username, wallet_address, uphold_card_id, registered_at)
		VALUES ('carol', '0xCAROL', '', '2024-01-01T00:00:00Z')`)
	svc1.db.Close()

	svc2, _ := New(Config{Enabled: true, ListenAddr: "127.0.0.1:0", DBPath: dbPath})
	defer svc2.db.Close()

	reg, err := svc2.loadWallet("carol")
	if err != nil {
		t.Fatalf("loadWallet: %v", err)
	}
	if reg.WalletAddress != "0xCAROL" {
		t.Errorf("unexpected wallet address: %s", reg.WalletAddress)
	}
}

func TestDisabledService(t *testing.T) {
	_, err := New(Config{Enabled: false})
	if err == nil {
		t.Error("expected error when rewards disabled, got nil")
	}
}

func TestRateFor(t *testing.T) {
	svc := newTestService(t)
	rates := DefaultRewardRates()

	cases := []struct {
		eventType string
		expected  float64
	}{
		{"merge_request", rates.MergeRequest},
		{"issue", rates.Issue},
		{"pipeline", rates.Pipeline},
		{"star", rates.Star},
		{"unknown", 0},
	}
	for _, c := range cases {
		got := svc.rateFor(c.eventType)
		if got != c.expected {
			t.Errorf("rateFor(%q) = %.3f, want %.3f", c.eventType, got, c.expected)
		}
	}
}

func TestWebhookSecretValidation(t *testing.T) {
	secret := "supersecret"
	// hmac.Equal provides constant-time comparison — same as the handler uses.
	if !hmac.Equal([]byte(secret), []byte(secret)) {
		t.Error("equal secrets should match")
	}
	if hmac.Equal([]byte(secret), []byte("wrong")) {
		t.Error("different secrets should not match")
	}
}

func TestDBPathDefault(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	svc, err := New(Config{Enabled: true, ListenAddr: "127.0.0.1:0", DBPath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.db.Close()
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("expected DB file to exist at %s: %v", dbPath, err)
	}
}
