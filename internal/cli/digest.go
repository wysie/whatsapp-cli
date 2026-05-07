package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/eddmann/whatsapp-cli/internal/store"
)

const defaultDigestExcludeJID = "270240063738098@lid"

var (
	digestSince      string
	digestAfter      string
	digestBefore     string
	digestExcludeJID []string
	digestLimit      int
	digestStaleAfter time.Duration
)

var digestCmd = &cobra.Command{
	Use:   "digest",
	Short: "Extract deterministic local message data for daily summaries",
	Long: `Extract local WhatsApp messages in chronological order for agent/cron summaries.

This command is local-only: it reads the SQLite message store and does not force a
network sync. Use 'whatsapp status -f json --no-auto-sync' first, or inspect the
included stale/latest-message fields in JSON output.`,
	RunE: runDigest,
}

type DigestCommandResult struct {
	Healthy                 bool               `json:"healthy"`
	Stale                   bool               `json:"stale"`
	StaleAfterSeconds       int64              `json:"stale_after_seconds"`
	LatestStoredMessageTime *time.Time         `json:"latest_stored_message_time,omitempty"`
	Digest                  store.DigestResult `json:"digest"`
}

func init() {
	rootCmd.AddCommand(digestCmd)
	digestCmd.Flags().StringVar(&digestSince, "since", "24h", "Digest window duration like 24h, or RFC3339 start time")
	digestCmd.Flags().StringVar(&digestAfter, "after", "", "Digest messages after timestamp (RFC3339); overrides --since")
	digestCmd.Flags().StringVar(&digestBefore, "before", "", "Digest messages before timestamp (RFC3339); defaults to now")
	digestCmd.Flags().StringSliceVar(&digestExcludeJID, "exclude", []string{defaultDigestExcludeJID}, "Chat JID to exclude; repeat or comma-separate")
	digestCmd.Flags().IntVar(&digestLimit, "limit", 5000, "Maximum messages to include")
	digestCmd.Flags().DurationVar(&digestStaleAfter, "stale-after", 15*time.Minute, "Mark output stale if newest stored message is older than this")
}

func runDigest(cmd *cobra.Command, args []string) error {
	if err := EnsureDirectories(); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	now := time.Now()
	after, before, err := digestWindow(now)
	if err != nil {
		return err
	}

	db, err := store.Open(GetMessagesDBPath())
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.CloseQuietly()

	digest, err := db.DigestMessages(store.DigestOptions{
		After:       after,
		Before:      before,
		ExcludeJIDs: digestExcludeJID,
		Limit:       digestLimit,
	})
	if err != nil {
		return fmt.Errorf("failed to build digest: %w", err)
	}

	result := DigestCommandResult{
		StaleAfterSeconds: int64(digestStaleAfter.Seconds()),
		Digest:            digest,
	}
	if latest, ok, err := db.GetLatestMessageTime(); err == nil && ok {
		result.LatestStoredMessageTime = &latest
		result.Stale = time.Since(latest) > digestStaleAfter
	} else {
		result.Stale = true
	}
	result.Healthy = !result.Stale

	if GetFormat() == FormatHuman {
		return outputDigestHuman(result)
	}
	return Output(result)
}

func digestWindow(now time.Time) (time.Time, time.Time, error) {
	before := now
	if digestBefore != "" {
		parsed, err := time.Parse(time.RFC3339, digestBefore)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --before timestamp: %w", err)
		}
		before = parsed
	}

	if digestAfter != "" {
		parsed, err := time.Parse(time.RFC3339, digestAfter)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --after timestamp: %w", err)
		}
		return parsed, before, nil
	}

	if digestSince == "" {
		return before.Add(-24 * time.Hour), before, nil
	}
	if duration, err := time.ParseDuration(digestSince); err == nil {
		return before.Add(-duration), before, nil
	}
	parsed, err := time.Parse(time.RFC3339, digestSince)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid --since value %q; use duration like 24h or RFC3339 timestamp", digestSince)
	}
	return parsed, before, nil
}

func outputDigestHuman(result DigestCommandResult) error {
	fmt.Fprintf(os.Stdout, "WhatsApp Digest\n")
	fmt.Fprintf(os.Stdout, "===============\n\n")
	fmt.Fprintf(os.Stdout, "Window:   %s → %s\n", result.Digest.After.Format(time.RFC3339), result.Digest.Before.Format(time.RFC3339))
	fmt.Fprintf(os.Stdout, "Healthy:  %v\n", result.Healthy)
	fmt.Fprintf(os.Stdout, "Stale:    %v\n", result.Stale)
	fmt.Fprintf(os.Stdout, "Chats:    %d\n", result.Digest.ChatCount)
	fmt.Fprintf(os.Stdout, "Messages: %d\n", result.Digest.MessageCount)
	if result.LatestStoredMessageTime != nil {
		fmt.Fprintf(os.Stdout, "Latest:   %s\n", result.LatestStoredMessageTime.Format(time.RFC3339))
	}
	if len(result.Digest.URLs) > 0 {
		fmt.Fprintf(os.Stdout, "URLs:     %d\n", len(result.Digest.URLs))
	}
	fmt.Fprintln(os.Stdout)
	for _, msg := range result.Digest.Messages {
		chat := msg.ChatJID
		if msg.ChatName != nil && *msg.ChatName != "" {
			chat = *msg.ChatName
		}
		sender := msg.Sender
		if msg.IsFromMe {
			sender = "You"
		} else if msg.SenderName != nil && *msg.SenderName != "" {
			sender = *msg.SenderName
		}
		content := ""
		if msg.Content != nil {
			content = strings.ReplaceAll(*msg.Content, "\n", " ")
		}
		if content == "" && msg.MediaType != nil {
			content = "[" + *msg.MediaType + "]"
		}
		fmt.Fprintf(os.Stdout, "[%s] [%s] %s: %s\n", msg.Timestamp.Format(time.RFC3339), chat, sender, content)
	}
	return nil
}
