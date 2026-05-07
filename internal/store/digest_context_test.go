package store

import (
	"strings"
	"testing"
	"time"
)

func strPtr(s string) *string { return &s }

func TestDigestSummaryContextPrioritizesURLsActionsAndEventsWithinMaxChars(t *testing.T) {
	base := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)
	result := DigestResult{
		After:        base.Add(-24 * time.Hour),
		Before:       base,
		MessageCount: 5,
		ChatCount:    3,
		URLs:         []string{"https://example.com/pay"},
		Messages: []Message{
			{ID: "low1", ChatJID: "chat-a", ChatName: strPtr("Chatter"), Sender: "111", SenderName: strPtr("Alice"), Content: strPtr(strings.Repeat("haha ", 80)), Timestamp: base.Add(-50 * time.Minute)},
			{ID: "url1", ChatJID: "chat-b", ChatName: strPtr("Links"), Sender: "222", SenderName: strPtr("Bob"), Content: strPtr("please check https://example.com/pay by tonight"), Timestamp: base.Add(-40 * time.Minute)},
			{ID: "action1", ChatJID: "chat-c", ChatName: strPtr("Work"), Sender: "333", SenderName: strPtr("Carol"), Content: strPtr("YC please confirm the booking and pay $120 by Friday"), Timestamp: base.Add(-30 * time.Minute)},
			{ID: "event1", ChatJID: "chat-c", ChatName: strPtr("Work"), Sender: "333", SenderName: strPtr("Carol"), Content: strPtr("Meeting tomorrow 3pm at Kinex"), Timestamp: base.Add(-20 * time.Minute)},
			{ID: "low2", ChatJID: "chat-a", ChatName: strPtr("Chatter"), Sender: "111", SenderName: strPtr("Alice"), Content: strPtr(strings.Repeat("ok ", 80)), Timestamp: base.Add(-10 * time.Minute)},
		},
	}

	ctx := BuildDigestSummaryContext(result, 900)

	for _, want := range []string{"SUMMARY_CONTEXT", "URLS:", "https://example.com/pay", "please confirm", "Meeting tomorrow 3pm", "ACTIVE_CHATS:", "MESSAGES:"} {
		if !strings.Contains(ctx, want) {
			t.Fatalf("expected context to contain %q, got:\n%s", want, ctx)
		}
	}
	if len(ctx) > 900 {
		t.Fatalf("expected context <= 900 chars, got %d", len(ctx))
	}
}

func TestDigestSummaryContextReportsTruncationWhenMessagesAreDropped(t *testing.T) {
	base := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)
	result := DigestResult{
		After:        base.Add(-24 * time.Hour),
		Before:       base,
		MessageCount: 3,
		ChatCount:    1,
		Messages: []Message{
			{ID: "a", ChatJID: "chat-a", ChatName: strPtr("A"), Sender: "111", Content: strPtr(strings.Repeat("alpha ", 60)), Timestamp: base.Add(-3 * time.Minute)},
			{ID: "b", ChatJID: "chat-a", ChatName: strPtr("A"), Sender: "111", Content: strPtr(strings.Repeat("beta ", 60)), Timestamp: base.Add(-2 * time.Minute)},
			{ID: "c", ChatJID: "chat-a", ChatName: strPtr("A"), Sender: "111", Content: strPtr(strings.Repeat("gamma ", 60)), Timestamp: base.Add(-1 * time.Minute)},
		},
	}

	ctx := BuildDigestSummaryContext(result, 500)
	if !strings.Contains(ctx, "TRUNCATED_MESSAGES:") {
		t.Fatalf("expected truncation marker, got:\n%s", ctx)
	}
	if len(ctx) > 500 {
		t.Fatalf("expected context <= 500 chars, got %d", len(ctx))
	}
}
