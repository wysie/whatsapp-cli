package cli

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/eddmann/whatsapp-cli/internal/store"
)

var (
	watchdogStaleAfter     time.Duration
	watchdogRestartService string
	watchdogWait           time.Duration
	watchdogQuiet          bool
)

type WatchdogDecision struct {
	Action        string `json:"action"`
	Reason        string `json:"reason,omitempty"`
	ShouldRestart bool   `json:"should_restart"`
	ShouldAlert   bool   `json:"should_alert"`
}

type WatchdogResult struct {
	Before    SyncStatus       `json:"before"`
	After     *SyncStatus      `json:"after,omitempty"`
	Decision  WatchdogDecision `json:"decision"`
	Restarted bool             `json:"restarted"`
	Recovered bool             `json:"recovered"`
	Error     string           `json:"error,omitempty"`
}

var watchdogCmd = &cobra.Command{
	Use:   "watchdog",
	Short: "Check sync health and optionally restart the sync service",
	Long: `Check local sync health without forcing a WhatsApp network sync.

When --restart-service is set, unhealthy/stale sync is restarted once and checked again.
With --quiet, healthy or recovered states print nothing; only unrecovered failures alert.`,
	RunE: runWatchdog,
}

func init() {
	rootCmd.AddCommand(watchdogCmd)
	watchdogCmd.Flags().DurationVar(&watchdogStaleAfter, "stale-after", 15*time.Minute, "Consider sync stale after this duration")
	watchdogCmd.Flags().StringVar(&watchdogRestartService, "restart-service", "", "launchd service label to restart once if unhealthy, e.g. com.whatsapp-cli.sync")
	watchdogCmd.Flags().DurationVar(&watchdogWait, "wait", 10*time.Second, "Wait after restart before re-checking status")
	watchdogCmd.Flags().BoolVar(&watchdogQuiet, "quiet", false, "Print nothing when healthy or recovered")
}

func runWatchdog(cmd *cobra.Command, args []string) error {
	status, err := collectSyncStatus(watchdogStaleAfter)
	if err != nil {
		return err
	}
	decision := decideWatchdogAction(status, watchdogRestartService != "")
	result := WatchdogResult{Before: status, Decision: decision}

	if decision.ShouldRestart {
		if err := restartLaunchdService(watchdogRestartService); err != nil {
			result.Error = err.Error()
			result.Decision.Action = "alert"
			result.Decision.ShouldAlert = true
			return outputWatchdogResult(result)
		}
		result.Restarted = true
		recordWatchdogRestart()
		time.Sleep(watchdogWait)
		after, err := collectSyncStatus(watchdogStaleAfter)
		if err != nil {
			result.Error = err.Error()
			result.Decision.Action = "alert"
			result.Decision.ShouldAlert = true
			return outputWatchdogResult(result)
		}
		result.After = &after
		result.Recovered = after.Healthy
		if after.Healthy {
			result.Decision = WatchdogDecision{Action: "recovered", Reason: "sync healthy after restart"}
		} else {
			result.Decision = WatchdogDecision{Action: "alert", Reason: watchdogReason(after), ShouldAlert: true}
		}
	}

	return outputWatchdogResult(result)
}

func decideWatchdogAction(status SyncStatus, canRestart bool) WatchdogDecision {
	if status.Healthy {
		return WatchdogDecision{Action: "none"}
	}
	reason := watchdogReason(status)
	if canRestart {
		return WatchdogDecision{Action: "restart", Reason: reason, ShouldRestart: true}
	}
	return WatchdogDecision{Action: "alert", Reason: reason, ShouldAlert: true}
}

func watchdogReason(status SyncStatus) string {
	var reasons []string
	if !status.Authenticated {
		reasons = append(reasons, "not authenticated")
	}
	if status.Stale {
		reasons = append(reasons, "stale")
	}
	if status.SyncProcessRunning != nil && !*status.SyncProcessRunning {
		reasons = append(reasons, "process not running")
	}
	if status.SyncState != "" && status.SyncState != "connected" && status.SyncState != "following" {
		reasons = append(reasons, "state="+status.SyncState)
	}
	if status.SyncLastError != "" {
		reasons = append(reasons, "last_error="+status.SyncLastError)
	}
	if len(reasons) == 0 {
		return "unhealthy"
	}
	return strings.Join(reasons, ", ")
}

func outputWatchdogResult(result WatchdogResult) error {
	if watchdogQuiet && (result.Decision.Action == "none" || result.Decision.Action == "recovered") {
		return nil
	}
	if IsJSON() {
		return Output(result)
	}
	if result.Decision.Action == "none" {
		fmt.Println("WhatsApp sync healthy")
		return nil
	}
	if result.Decision.Action == "recovered" {
		fmt.Println("WhatsApp sync recovered after restart")
		return nil
	}
	fmt.Printf("WhatsApp sync watchdog: %s (%s)\n", result.Decision.Action, result.Decision.Reason)
	return nil
}

func recordWatchdogRestart() {
	db, err := store.Open(GetMessagesDBPath())
	if err != nil {
		return
	}
	defer db.CloseQuietly()
	current := 0
	if raw, ok, err := db.GetMetadata("sync_watchdog_restart_count"); err == nil && ok {
		current = atoi(raw)
	}
	_ = db.SetMetadata("sync_watchdog_restart_count", fmt.Sprintf("%d", current+1))
	_ = db.SetMetadata("sync_last_watchdog_restart_at", time.Now().Format(time.RFC3339))
}

func restartLaunchdService(label string) error {
	if strings.TrimSpace(label) == "" {
		return fmt.Errorf("restart service label is required")
	}
	uidOut, err := exec.Command("id", "-u").Output()
	if err != nil {
		return fmt.Errorf("failed to get uid: %w", err)
	}
	uid := strings.TrimSpace(string(uidOut))
	target := "gui/" + uid + "/" + label
	_ = exec.Command("launchctl", "stop", target).Run()
	if err := exec.Command("launchctl", "start", target).Run(); err != nil {
		// Fall back to non-gui label for older/local launchctl invocations.
		_ = exec.Command("launchctl", "stop", label).Run()
		if err2 := exec.Command("launchctl", "start", label).Run(); err2 != nil {
			return fmt.Errorf("failed to restart %s: %w; fallback: %v", label, err, err2)
		}
	}
	return nil
}
