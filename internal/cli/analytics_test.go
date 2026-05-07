package cli

import "testing"

func TestHealthReportUsesReconnectMetadata(t *testing.T) {
	status := SyncStatus{Healthy: true, SyncState: "connected", LatestMessageAgeSec: ptrInt64(42)}
	meta := map[string]string{
		"sync_reconnect_count":        "3",
		"sync_watchdog_restart_count": "2",
		"sync_last_error":             "",
	}
	report := buildHealthReport(status, meta)
	if !report.Healthy || report.ReconnectCount != 3 || report.WatchdogRestartCount != 2 || report.LatestMessageAgeSeconds == nil || *report.LatestMessageAgeSeconds != 42 {
		t.Fatalf("unexpected health report: %+v", report)
	}
}

func TestMaintenancePlanRequiresExplicitAction(t *testing.T) {
	plan := buildMaintenancePlan(false, false, false, false)
	if len(plan.Actions) != 0 || plan.HasActions {
		t.Fatalf("empty flags should produce no actions: %+v", plan)
	}
	plan = buildMaintenancePlan(true, true, true, true)
	if !plan.HasActions || len(plan.Actions) != 4 {
		t.Fatalf("expected four actions: %+v", plan)
	}
}

func ptrInt64(v int64) *int64 { return &v }
