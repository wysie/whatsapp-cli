package cli

import (
	"strings"
	"testing"
	"time"
)

func TestNextReconnectDelayExponentialBackoffCapped(t *testing.T) {
	initial := 5 * time.Second
	max := 30 * time.Second
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 5 * time.Second},
		{2, 10 * time.Second},
		{3, 20 * time.Second},
		{4, 30 * time.Second},
		{8, 30 * time.Second},
	}
	for _, tc := range cases {
		if got := nextReconnectDelay(tc.attempt, initial, max); got != tc.want {
			t.Fatalf("attempt %d: got %s want %s", tc.attempt, got, tc.want)
		}
	}
}

func TestFollowReconnectConfigValidation(t *testing.T) {
	cfg := followReconnectConfig{Enabled: true, InitialDelay: 0, MaxDelay: time.Second, CheckInterval: 0, MaxAttempts: -1}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "initial delay") {
		t.Fatalf("expected initial delay validation error, got %v", err)
	}

	cfg = followReconnectConfig{Enabled: true, InitialDelay: time.Second, MaxDelay: 500 * time.Millisecond, CheckInterval: time.Second, MaxAttempts: 0}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "max delay") {
		t.Fatalf("expected max delay validation error, got %v", err)
	}

	cfg = followReconnectConfig{Enabled: true, InitialDelay: time.Second, MaxDelay: 2 * time.Second, CheckInterval: time.Second, MaxAttempts: 0}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
}
