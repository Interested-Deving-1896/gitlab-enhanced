package rewards

import (
	"testing"
)

func TestQueueReward(t *testing.T) {
	svc, err := New(Config{
		Enabled:       true,
		PublisherID:   "test-publisher",
		WalletAddress: "0xDEADBEEF",
		ListenAddr:    "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	event := ContributionEvent{
		Type:                "merge_request",
		ProjectID:           1,
		ContributorUsername: "alice",
		ObjectID:            42,
	}
	reward := svc.QueueReward(event)

	if reward.AmountBAT != DefaultRewardRates().MergeRequest {
		t.Errorf("expected %.2f BAT, got %.2f", DefaultRewardRates().MergeRequest, reward.AmountBAT)
	}
	if reward.Status != "pending" {
		t.Errorf("expected status pending, got %s", reward.Status)
	}
	if len(svc.pending) != 1 {
		t.Errorf("expected 1 pending reward, got %d", len(svc.pending))
	}
}

func TestDisabledService(t *testing.T) {
	_, err := New(Config{Enabled: false})
	if err == nil {
		t.Error("expected error when rewards disabled, got nil")
	}
}

func TestRateFor(t *testing.T) {
	svc, _ := New(Config{Enabled: true, ListenAddr: "127.0.0.1:0"})
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
