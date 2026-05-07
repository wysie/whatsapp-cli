package cli

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/eddmann/whatsapp-cli/internal/store"
	"github.com/eddmann/whatsapp-cli/internal/whatsapp"
)

var statusStaleAfter time.Duration

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show local sync freshness and daemon health",
	Long: `Show local WhatsApp CLI health without forcing a network sync.

This is intended for cron jobs, agents, and launchd/systemd watchdogs that need
to know whether the local database is fresh enough to trust.`,
	RunE: runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
	statusCmd.Flags().DurationVar(&statusStaleAfter, "stale-after", 15*time.Minute, "Consider sync stale if latest local message/event is older than this")
}

type SyncStatus struct {
	Healthy             bool       `json:"healthy"`
	Authenticated       bool       `json:"authenticated"`
	SyncState           string     `json:"sync_state,omitempty"`
	SyncMode            string     `json:"sync_mode,omitempty"`
	SyncPID             string     `json:"sync_pid,omitempty"`
	SyncProcessRunning  *bool      `json:"sync_process_running,omitempty"`
	SyncStartedAt       *time.Time `json:"sync_started_at,omitempty"`
	SyncConnectedAt     *time.Time `json:"sync_connected_at,omitempty"`
	SyncHeartbeatAt     *time.Time `json:"sync_heartbeat_at,omitempty"`
	SyncLastEventAt     *time.Time `json:"sync_last_event_at,omitempty"`
	SyncLastMessageSeen *time.Time `json:"sync_last_message_seen_at,omitempty"`
	SyncCompletedAt     *time.Time `json:"sync_completed_at,omitempty"`
	SyncStoppedAt       *time.Time `json:"sync_stopped_at,omitempty"`
	SyncLastError       string     `json:"sync_last_error,omitempty"`
	LatestMessageTime   *time.Time `json:"latest_message_time,omitempty"`
	LatestMessageAgeSec *int64     `json:"latest_message_age_seconds,omitempty"`
	StaleAfterSeconds   int64      `json:"stale_after_seconds"`
	Stale               bool       `json:"stale"`
	Chats               int        `json:"chats"`
	Messages            int        `json:"messages"`
}

func runStatus(cmd *cobra.Command, args []string) error {
	if err := EnsureDirectories(); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	db, err := store.Open(GetMessagesDBPath())
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.CloseQuietly()

	client, err := whatsapp.New(db, GetStoreDir(), false, nil)
	authenticated := err == nil && client.IsAuthenticated()

	chatCount, _ := db.CountChats("")
	msgCount, _ := db.CountMessages()
	status := SyncStatus{
		Authenticated:     authenticated,
		StaleAfterSeconds: int64(statusStaleAfter.Seconds()),
		Chats:             chatCount,
		Messages:          msgCount,
	}

	status.SyncState = metadataString(db, "sync_state")
	status.SyncMode = metadataString(db, "sync_mode")
	status.SyncPID = metadataString(db, "sync_pid")
	status.SyncLastError = metadataString(db, "sync_last_error")
	status.SyncStartedAt = metadataTime(db, "sync_started_at")
	status.SyncConnectedAt = metadataTime(db, "sync_connected_at")
	status.SyncHeartbeatAt = metadataTime(db, "sync_heartbeat_at")
	status.SyncLastEventAt = metadataTime(db, "sync_last_event_at")
	status.SyncLastMessageSeen = metadataTime(db, "sync_last_message_seen_at")
	status.SyncCompletedAt = metadataTime(db, "sync_completed_at")
	status.SyncStoppedAt = metadataTime(db, "sync_stopped_at")

	if status.SyncPID != "" {
		running := processRunning(status.SyncPID)
		status.SyncProcessRunning = &running
	}

	if latest, ok, err := db.GetLatestMessageTime(); err == nil && ok {
		status.LatestMessageTime = &latest
		age := int64(time.Since(latest).Seconds())
		status.LatestMessageAgeSec = &age
		status.Stale = time.Since(latest) > statusStaleAfter
	} else {
		status.Stale = true
	}

	if status.SyncHeartbeatAt != nil && time.Since(*status.SyncHeartbeatAt) > statusStaleAfter {
		status.Stale = true
	}
	if status.SyncLastError != "" || status.SyncState == "logged_out" || status.SyncState == "timeout" {
		status.Healthy = false
	} else {
		processOK := status.SyncProcessRunning == nil || *status.SyncProcessRunning
		status.Healthy = authenticated && !status.Stale && processOK
	}

	if IsJSON() {
		return Output(status)
	}

	fmt.Printf("WhatsApp CLI Status\n")
	fmt.Printf("===================\n\n")
	fmt.Printf("Authenticated: %v\n", status.Authenticated)
	fmt.Printf("Healthy:       %v\n", status.Healthy)
	fmt.Printf("Stale:         %v\n", status.Stale)
	fmt.Printf("Chats:         %d\n", status.Chats)
	fmt.Printf("Messages:      %d\n", status.Messages)
	if status.LatestMessageTime != nil {
		fmt.Printf("Latest msg:    %s\n", status.LatestMessageTime.Format(time.RFC3339))
	}
	if status.SyncState != "" {
		fmt.Printf("Sync state:    %s\n", status.SyncState)
	}
	if status.SyncPID != "" {
		fmt.Printf("Sync PID:      %s", status.SyncPID)
		if status.SyncProcessRunning != nil {
			fmt.Printf(" (running=%v)", *status.SyncProcessRunning)
		}
		fmt.Println()
	}
	if status.SyncHeartbeatAt != nil {
		fmt.Printf("Heartbeat:     %s\n", status.SyncHeartbeatAt.Format(time.RFC3339))
	}
	if status.SyncLastError != "" {
		fmt.Printf("Last error:    %s\n", status.SyncLastError)
	}
	return nil
}

func metadataString(db *store.DB, key string) string {
	v, ok, err := db.GetMetadata(key)
	if err != nil || !ok {
		return ""
	}
	return v
}

func metadataTime(db *store.DB, key string) *time.Time {
	v := metadataString(db, key)
	if v == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return nil
	}
	return &t
}

func processRunning(pidString string) bool {
	pid, err := strconv.Atoi(pidString)
	if err != nil || pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil || proc == nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
