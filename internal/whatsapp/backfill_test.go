package whatsapp

import (
	"testing"
	"time"

	"github.com/eddmann/whatsapp-cli/internal/store"
	waStore "go.mau.fi/whatsmeow/store"
)

func TestBuildBackfillAnchorUsesOldestStoredMessage(t *testing.T) {
	oldest := store.Message{
		ID:        "anchor-id",
		ChatJID:   "12345@s.whatsapp.net",
		Sender:    "67890",
		Timestamp: time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC),
		IsFromMe:  false,
	}

	anchor, err := buildBackfillAnchor(oldest)
	if err != nil {
		t.Fatalf("build anchor: %v", err)
	}

	if anchor.ID != "anchor-id" {
		t.Fatalf("expected ID anchor-id, got %q", anchor.ID)
	}
	if anchor.Chat.String() != "12345@s.whatsapp.net" {
		t.Fatalf("expected chat JID, got %s", anchor.Chat.String())
	}
	if !anchor.Timestamp.Equal(oldest.Timestamp) {
		t.Fatalf("expected timestamp %s, got %s", oldest.Timestamp, anchor.Timestamp)
	}
	if anchor.IsFromMe {
		t.Fatalf("expected incoming anchor to preserve is_from_me=false")
	}
}

func TestBuildBackfillAnchorRejectsMissingMessageID(t *testing.T) {
	_, err := buildBackfillAnchor(store.Message{ChatJID: "12345@s.whatsapp.net"})
	if err == nil {
		t.Fatalf("expected missing message ID error")
	}
}

func TestBackfillStorageChatJIDUsesRequestedJIDForOnDemandResponse(t *testing.T) {
	got := backfillStorageChatJID("233564700451061@lid", "6581632144@s.whatsapp.net", true)
	if got != "233564700451061@lid" {
		t.Fatalf("expected requested LID to be used for storage, got %q", got)
	}
}

func TestBackfillStorageChatJIDUsesResponseJIDWhenNoRequestIsPending(t *testing.T) {
	got := backfillStorageChatJID("", "6581632144@s.whatsapp.net", true)
	if got != "6581632144@s.whatsapp.net" {
		t.Fatalf("expected response JID when no request pending, got %q", got)
	}
}

func TestConfigureFullHistorySyncSetsPairingPayload(t *testing.T) {
	originalRequireFullSync := waStore.DeviceProps.RequireFullSync
	originalConfig := waStore.DeviceProps.HistorySyncConfig
	t.Cleanup(func() {
		waStore.DeviceProps.RequireFullSync = originalRequireFullSync
		waStore.DeviceProps.HistorySyncConfig = originalConfig
	})

	ConfigureFullHistorySync(3650, 10240)

	if !waStore.DeviceProps.GetRequireFullSync() {
		t.Fatalf("expected RequireFullSync to be enabled")
	}
	if waStore.DeviceProps.GetHistorySyncConfig().GetFullSyncDaysLimit() != 3650 {
		t.Fatalf("expected 3650 full sync days, got %d", waStore.DeviceProps.GetHistorySyncConfig().GetFullSyncDaysLimit())
	}
	if waStore.DeviceProps.GetHistorySyncConfig().GetFullSyncSizeMbLimit() != 10240 {
		t.Fatalf("expected 10240MB size limit, got %d", waStore.DeviceProps.GetHistorySyncConfig().GetFullSyncSizeMbLimit())
	}
}
