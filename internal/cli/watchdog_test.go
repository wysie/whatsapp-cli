package cli

import (
	"strings"
	"testing"
	"time"
)

func TestWatchdogDecisionHealthyIsSilent(t *testing.T) {
	running := true
	status := SyncStatus{Healthy: true, Authenticated: true, Stale: false, SyncProcessRunning: &running}
	decision := decideWatchdogAction(status, false)
	if decision.Action != "none" || decision.ShouldRestart || decision.ShouldAlert {
		t.Fatalf("expected no action for healthy status, got %#v", decision)
	}
}

func TestWatchdogDecisionRestartsUnhealthyWhenConfigured(t *testing.T) {
	running := false
	status := SyncStatus{Healthy: false, Authenticated: true, Stale: true, SyncState: "connected", SyncProcessRunning: &running, SyncLastError: ""}
	decision := decideWatchdogAction(status, true)
	if decision.Action != "restart" || !decision.ShouldRestart || decision.ShouldAlert {
		t.Fatalf("expected restart without alert before retry, got %#v", decision)
	}
	if !strings.Contains(decision.Reason, "stale") || !strings.Contains(decision.Reason, "process") {
		t.Fatalf("expected reason to mention stale/process, got %q", decision.Reason)
	}
}

func TestWatchdogDecisionAlertsWhenRestartDisabled(t *testing.T) {
	latest := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)
	status := SyncStatus{Healthy: false, Authenticated: true, Stale: true, LatestMessageTime: &latest}
	decision := decideWatchdogAction(status, false)
	if decision.Action != "alert" || decision.ShouldRestart || !decision.ShouldAlert {
		t.Fatalf("expected alert when restart disabled, got %#v", decision)
	}
}

func TestNewestSyncFreshnessTimeUsesHeartbeatWhenMessagesIdle(t *testing.T) {
	oldMessage := time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC)
	freshHeartbeat := time.Date(2026, 5, 8, 1, 30, 0, 0, time.UTC)
	freshness := newestSyncFreshnessTime(&oldMessage, &freshHeartbeat)
	if freshness == nil || !freshness.Equal(freshHeartbeat) {
		t.Fatalf("expected fresh heartbeat to win over idle latest message, got %v", freshness)
	}
}
