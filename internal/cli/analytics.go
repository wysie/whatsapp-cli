package cli

import (
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/eddmann/whatsapp-cli/internal/store"
)

var (
	healthReportStaleAfter time.Duration
	coverageChatLimit      int
	maintenanceBackup      bool
	maintenanceIntegrity   bool
	maintenanceRebuildFTS  bool
	maintenanceVacuum      bool
	maintenanceBackupDir   string
)

type HealthReport struct {
	Healthy                 bool       `json:"healthy"`
	Authenticated           bool       `json:"authenticated"`
	Stale                   bool       `json:"stale"`
	SyncState               string     `json:"sync_state,omitempty"`
	SyncMode                string     `json:"sync_mode,omitempty"`
	SyncPID                 string     `json:"sync_pid,omitempty"`
	SyncProcessRunning      *bool      `json:"sync_process_running,omitempty"`
	LatestMessageTime       *time.Time `json:"latest_message_time,omitempty"`
	LatestMessageAgeSeconds *int64     `json:"latest_message_age_seconds,omitempty"`
	ReconnectCount          int        `json:"reconnect_count"`
	WatchdogRestartCount    int        `json:"watchdog_restart_count"`
	LastReconnectAt         string     `json:"last_reconnect_at,omitempty"`
	LastWatchdogRestartAt   string     `json:"last_watchdog_restart_at,omitempty"`
	LastError               string     `json:"last_error,omitempty"`
	Chats                   int        `json:"chats"`
	Messages                int        `json:"messages"`
}

type MaintenancePlan struct {
	Actions    []string `json:"actions"`
	HasActions bool     `json:"has_actions"`
}

type MaintenanceCommandResult struct {
	Plan    MaintenancePlan           `json:"plan"`
	Results []store.MaintenanceResult `json:"results"`
	Healthy bool                      `json:"healthy"`
}

var healthReportCmd = &cobra.Command{
	Use:   "health-report",
	Short: "Show sync reliability health, reconnects, watchdog restarts, and freshness",
	RunE:  runHealthReport,
}

var coverageCmd = &cobra.Command{
	Use:   "coverage",
	Short: "Audit WhatsApp archive coverage by month and chat",
	RunE:  runCoverage,
}

var maintenanceCmd = &cobra.Command{
	Use:   "maintenance",
	Short: "Run safe database maintenance: backup, integrity check, FTS rebuild, vacuum",
	RunE:  runMaintenance,
}

func init() {
	rootCmd.AddCommand(healthReportCmd)
	healthReportCmd.Flags().DurationVar(&healthReportStaleAfter, "stale-after", 15*time.Minute, "Consider sync stale after this duration")

	rootCmd.AddCommand(coverageCmd)
	coverageCmd.Flags().IntVar(&coverageChatLimit, "chat-limit", 25, "Maximum per-chat coverage rows to include")

	rootCmd.AddCommand(maintenanceCmd)
	maintenanceCmd.Flags().BoolVar(&maintenanceBackup, "backup", false, "Back up the messages database")
	maintenanceCmd.Flags().StringVar(&maintenanceBackupDir, "backup-dir", "", "Directory for --backup output; default is store/backups")
	maintenanceCmd.Flags().BoolVar(&maintenanceIntegrity, "integrity-check", false, "Run PRAGMA integrity_check")
	maintenanceCmd.Flags().BoolVar(&maintenanceRebuildFTS, "rebuild-fts", false, "Rebuild the FTS5 search index")
	maintenanceCmd.Flags().BoolVar(&maintenanceVacuum, "vacuum", false, "Run VACUUM after other actions")
}

func buildHealthReport(status SyncStatus, meta map[string]string) HealthReport {
	return HealthReport{
		Healthy:                 status.Healthy,
		Authenticated:           status.Authenticated,
		Stale:                   status.Stale,
		SyncState:               status.SyncState,
		SyncMode:                status.SyncMode,
		SyncPID:                 status.SyncPID,
		SyncProcessRunning:      status.SyncProcessRunning,
		LatestMessageTime:       status.LatestMessageTime,
		LatestMessageAgeSeconds: status.LatestMessageAgeSec,
		ReconnectCount:          atoi(meta["sync_reconnect_count"]),
		WatchdogRestartCount:    atoi(meta["sync_watchdog_restart_count"]),
		LastReconnectAt:         meta["sync_last_reconnect_at"],
		LastWatchdogRestartAt:   meta["sync_last_watchdog_restart_at"],
		LastError:               firstNonEmpty(status.SyncLastError, meta["sync_last_error"]),
		Chats:                   status.Chats,
		Messages:                status.Messages,
	}
}

func buildMaintenancePlan(backup, integrity, rebuildFTS, vacuum bool) MaintenancePlan {
	actions := []string{}
	if backup {
		actions = append(actions, "backup")
	}
	if integrity {
		actions = append(actions, "integrity_check")
	}
	if rebuildFTS {
		actions = append(actions, "rebuild_fts")
	}
	if vacuum {
		actions = append(actions, "vacuum")
	}
	return MaintenancePlan{Actions: actions, HasActions: len(actions) > 0}
}

func runHealthReport(cmd *cobra.Command, args []string) error {
	status, err := collectSyncStatus(healthReportStaleAfter)
	if err != nil {
		return err
	}
	db, err := store.Open(GetMessagesDBPath())
	if err != nil {
		return err
	}
	defer db.CloseQuietly()
	meta := map[string]string{}
	for _, key := range []string{"sync_reconnect_count", "sync_watchdog_restart_count", "sync_last_reconnect_at", "sync_last_watchdog_restart_at", "sync_last_error"} {
		if v, ok, err := db.GetMetadata(key); err == nil && ok {
			meta[key] = v
		}
	}
	report := buildHealthReport(status, meta)
	if IsJSON() {
		return Output(report)
	}
	fmt.Println("WhatsApp Sync Health Report")
	fmt.Println("===========================")
	fmt.Println()
	fmt.Printf("Healthy:            %v\n", report.Healthy)
	fmt.Printf("Stale:              %v\n", report.Stale)
	fmt.Printf("Sync state:         %s\n", report.SyncState)
	fmt.Printf("Reconnects:         %d\n", report.ReconnectCount)
	fmt.Printf("Watchdog restarts:  %d\n", report.WatchdogRestartCount)
	if report.LatestMessageAgeSeconds != nil {
		fmt.Printf("Latest msg age:     %ds\n", *report.LatestMessageAgeSeconds)
	}
	if report.LastError != "" {
		fmt.Printf("Last error:         %s\n", report.LastError)
	}
	return nil
}

func runCoverage(cmd *cobra.Command, args []string) error {
	db, err := store.Open(GetMessagesDBPath())
	if err != nil {
		return err
	}
	defer db.CloseQuietly()
	report, err := db.CoverageReport(coverageChatLimit)
	if err != nil {
		return err
	}
	if IsJSON() {
		return Output(report)
	}
	fmt.Println("WhatsApp Archive Coverage")
	fmt.Println("=========================")
	fmt.Println()
	fmt.Printf("Messages: %d\nChats:    %d\n", report.TotalMessages, report.TotalChats)
	if report.FirstMessage != nil && report.LastMessage != nil {
		fmt.Printf("Range:    %s → %s\n", report.FirstMessage.Format(time.RFC3339), report.LastMessage.Format(time.RFC3339))
	}
	fmt.Println("\nMonths:")
	for _, m := range report.Months {
		fmt.Printf("- %s: %d messages across %d chats\n", m.Month, m.Messages, m.Chats)
	}
	fmt.Println("\nTop chats:")
	for _, c := range report.Chats {
		name := c.ChatName
		if name == "" {
			name = c.ChatJID
		}
		fmt.Printf("- %s: %d messages", name, c.Messages)
		if c.FirstSeen != nil && c.LastSeen != nil {
			fmt.Printf(" (%s → %s)", c.FirstSeen.Format("2006-01-02"), c.LastSeen.Format("2006-01-02"))
		}
		fmt.Println()
	}
	return nil
}

func runMaintenance(cmd *cobra.Command, args []string) error {
	plan := buildMaintenancePlan(maintenanceBackup, maintenanceIntegrity, maintenanceRebuildFTS, maintenanceVacuum)
	result := MaintenanceCommandResult{Plan: plan, Healthy: true}
	if !plan.HasActions {
		result.Healthy = false
		if IsJSON() {
			return Output(result)
		}
		fmt.Println("No maintenance action selected. Use --backup, --integrity-check, --rebuild-fts, or --vacuum.")
		return nil
	}
	dbPath := GetMessagesDBPath()
	if maintenanceBackup {
		path, err := store.BackupDatabase(dbPath, maintenanceBackupDir)
		appendMaintenance(&result, "backup", path, err)
	}
	db, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer db.CloseQuietly()
	if maintenanceIntegrity {
		integrity, err := db.IntegrityCheck()
		if err != nil {
			appendMaintenance(&result, "integrity_check", "", err)
		} else if !integrity.OK {
			appendMaintenance(&result, "integrity_check", "", fmt.Errorf("integrity check failed: %v", integrity.Messages))
		} else {
			appendMaintenance(&result, "integrity_check", "", nil)
		}
	}
	if maintenanceRebuildFTS {
		appendMaintenance(&result, "rebuild_fts", "", db.RebuildFTS())
	}
	if maintenanceVacuum {
		appendMaintenance(&result, "vacuum", "", db.Vacuum())
	}
	if IsJSON() {
		return Output(result)
	}
	fmt.Println("WhatsApp DB Maintenance")
	fmt.Println("=======================")
	for _, item := range result.Results {
		status := "OK"
		if !item.OK {
			status = "FAIL"
		}
		fmt.Printf("[%s] %s", status, item.Action)
		if item.Path != "" {
			fmt.Printf(" -> %s", item.Path)
		}
		if item.Error != "" {
			fmt.Printf(" (%s)", item.Error)
		}
		fmt.Println()
	}
	return nil
}

func appendMaintenance(result *MaintenanceCommandResult, action, path string, err error) {
	item := store.MaintenanceResult{Action: action, OK: err == nil, Path: path}
	if err != nil {
		item.Error = err.Error()
		result.Healthy = false
	}
	result.Results = append(result.Results, item)
}

func atoi(value string) int {
	if value == "" {
		return 0
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return parsed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
