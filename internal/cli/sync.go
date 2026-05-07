package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/eddmann/whatsapp-cli/internal/store"
	"github.com/eddmann/whatsapp-cli/internal/whatsapp"
)

var (
	syncFollow                   bool
	syncDownloadMedia            bool
	syncReconnect                bool
	syncReconnectInitialDelay    time.Duration
	syncReconnectMaxDelay        time.Duration
	syncReconnectCheckInterval   time.Duration
	syncReconnectStaleEventAfter time.Duration
	syncReconnectMaxAttempts     int
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync messages from WhatsApp",
	Long: `Connect to WhatsApp and sync new messages to the local database.

By default, performs a one-time sync and exits.
Use --follow to run continuously and capture messages in real-time.`,
	RunE: runSync,
}

func init() {
	rootCmd.AddCommand(syncCmd)
	syncCmd.Flags().BoolVar(&syncFollow, "follow", false, "Run continuously, syncing messages in real-time")
	syncCmd.Flags().BoolVar(&syncDownloadMedia, "download-media", false, "Automatically download media files")
	syncCmd.Flags().BoolVar(&syncReconnect, "reconnect", true, "Automatically reconnect in --follow mode when disconnected")
	syncCmd.Flags().DurationVar(&syncReconnectInitialDelay, "reconnect-delay", 5*time.Second, "Initial reconnect backoff delay")
	syncCmd.Flags().DurationVar(&syncReconnectMaxDelay, "reconnect-max-delay", 2*time.Minute, "Maximum reconnect backoff delay")
	syncCmd.Flags().DurationVar(&syncReconnectCheckInterval, "reconnect-check-interval", 10*time.Second, "How often --follow checks connection health")
	syncCmd.Flags().DurationVar(&syncReconnectStaleEventAfter, "reconnect-stale-event-after", 15*time.Minute, "Reconnect if no WhatsApp events are observed for this long; 0 disables event-stale reconnect")
	syncCmd.Flags().IntVar(&syncReconnectMaxAttempts, "reconnect-max-attempts", 0, "Maximum reconnect attempts before exiting; 0 means unlimited")
}

func runSync(cmd *cobra.Command, args []string) error {
	if err := EnsureDirectories(); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	db, err := store.Open(GetMessagesDBPath())
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.CloseQuietly()

	client, err := whatsapp.New(db, GetStoreDir(), IsVerbose(), nil)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	if !client.IsAuthenticated() {
		return fmt.Errorf("not authenticated. Run 'whatsapp auth login' first")
	}

	_ = db.SetMetadata("sync_state", "starting")
	_ = db.SetMetadata("sync_mode", syncModeName(syncFollow))
	_ = db.SetMetadata("sync_pid", strconv.Itoa(os.Getpid()))
	_ = db.SetMetadata("sync_started_at", time.Now().Format(time.RFC3339))

	// Set up signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		signal.Stop(sigChan)
		fmt.Fprintln(os.Stderr, "\nInterrupted, disconnecting...")
		cancel()
	}()

	// Connect
	if err := client.Connect(); err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}

	if syncFollow {
		cfg := followReconnectConfig{
			Enabled:         syncReconnect,
			InitialDelay:    syncReconnectInitialDelay,
			MaxDelay:        syncReconnectMaxDelay,
			CheckInterval:   syncReconnectCheckInterval,
			StaleEventAfter: syncReconnectStaleEventAfter,
			MaxAttempts:     syncReconnectMaxAttempts,
		}
		if err := cfg.Validate(); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "Connected. Syncing messages continuously. Press Ctrl+C to stop.")
		_ = db.SetMetadata("sync_state", "following")
		stopHeartbeat := startSyncHeartbeat(db)
		defer stopHeartbeat()

		if err := runFollowLoop(ctx, db, client, cfg); err != nil {
			return err
		}
		_ = db.SetMetadata("sync_state", "stopped")
		_ = db.SetMetadata("sync_stopped_at", time.Now().Format(time.RFC3339))
	} else {
		fmt.Fprintln(os.Stderr, "Connected. Performing one-time sync...")

		// Wait for sync completion, timeout, or interrupt
		syncTimeout := 2 * time.Minute
		select {
		case <-ctx.Done():
			// User interrupted
			_ = db.SetMetadata("sync_state", "interrupted")
			_ = db.SetMetadata("sync_last_error", "interrupted")
		case <-client.SyncComplete:
			fmt.Fprintln(os.Stderr, "History sync complete.")
			_ = db.SetMetadata("sync_state", "completed")
			_ = db.SetMetadata("sync_completed_at", time.Now().Format(time.RFC3339))
			_ = db.SetMetadata("sync_last_error", "")
		case <-time.After(syncTimeout):
			fmt.Fprintln(os.Stderr, "Sync timeout reached.")
			_ = db.SetMetadata("sync_state", "timeout")
			_ = db.SetMetadata("sync_last_error", "sync timeout reached")
		}
	}

	client.Disconnect()

	// Update last sync time
	_ = db.SetLastSyncTime(time.Now())

	// Output stats
	chatCount, _ := db.CountChats("")
	msgCount, _ := db.CountMessages()

	return OutputResult(map[string]any{
		"chats":    chatCount,
		"messages": msgCount,
	}, fmt.Sprintf("Synced %d chats, %d messages", chatCount, msgCount))
}

func syncModeName(follow bool) string {
	if follow {
		return "follow"
	}
	return "once"
}

func runFollowLoop(ctx context.Context, db *store.DB, client *whatsapp.Client, cfg followReconnectConfig) error {
	if !cfg.Enabled {
		<-ctx.Done()
		return nil
	}

	ticker := time.NewTicker(cfg.CheckInterval)
	defer ticker.Stop()
	attempt := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			needsReconnect := !client.IsConnected()
			if !needsReconnect && cfg.StaleEventAfter > 0 {
				if lastEvent := metadataTime(db, "sync_last_event_at"); lastEvent != nil && time.Since(*lastEvent) > cfg.StaleEventAfter {
					needsReconnect = true
					_ = db.SetMetadata("sync_last_error", "no WhatsApp events observed since "+lastEvent.Format(time.RFC3339))
				}
			}
			if !needsReconnect {
				attempt = 0
				continue
			}
			attempt++
			if cfg.MaxAttempts > 0 && attempt > cfg.MaxAttempts {
				_ = db.SetMetadata("sync_state", "reconnect_failed")
				return fmt.Errorf("maximum reconnect attempts reached")
			}
			delay := nextReconnectDelay(attempt, cfg.InitialDelay, cfg.MaxDelay)
			_ = db.SetMetadata("sync_state", "reconnecting")
			_ = db.SetMetadata("sync_reconnect_count", strconv.Itoa(attempt))
			_ = db.SetMetadata("sync_last_reconnect_at", time.Now().Format(time.RFC3339))
			fmt.Fprintf(os.Stderr, "WhatsApp connection unhealthy; reconnect attempt %d in %s...\n", attempt, delay)
			client.Disconnect()
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(delay):
			}
			if err := client.Connect(); err != nil {
				_ = db.SetMetadata("sync_last_error", err.Error())
				fmt.Fprintf(os.Stderr, "Reconnect attempt %d failed: %v\n", attempt, err)
				continue
			}
			_ = db.SetMetadata("sync_state", "connected")
			_ = db.SetMetadata("sync_last_error", "")
			fmt.Fprintln(os.Stderr, "Reconnected to WhatsApp.")
			attempt = 0
		}
	}
}

func startSyncHeartbeat(db *store.DB) func() {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = db.SetMetadata("sync_heartbeat_at", time.Now().Format(time.RFC3339))
			}
		}
	}()
	_ = db.SetMetadata("sync_heartbeat_at", time.Now().Format(time.RFC3339))
	return cancel
}
