package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDigestMessagesReturnsAscendingMessagesWithinWindowAndExcludesChats(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "messages.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.CloseQuietly()

	alpha := "111@s.whatsapp.net"
	beta := "222@s.whatsapp.net"
	excluded := "333@s.whatsapp.net"
	_, err = db.Messages.Exec(`INSERT INTO chats (jid, name) VALUES (?, ?), (?, ?), (?, ?)`, alpha, "Alpha", beta, "Beta", excluded, "Excluded")
	if err != nil {
		t.Fatalf("insert chats: %v", err)
	}

	base := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)
	insertMessage := func(id, chat, sender, content string, ts time.Time, fromMe bool) {
		t.Helper()
		_, err := db.Messages.Exec(`INSERT INTO messages (id, chat_jid, sender, content, timestamp, is_from_me) VALUES (?, ?, ?, ?, ?, ?)`, id, chat, sender, content, ts, fromMe)
		if err != nil {
			t.Fatalf("insert message %s: %v", id, err)
		}
	}
	insertMessage("old", alpha, "111", "too old", base.Add(-3*time.Hour), false)
	insertMessage("b2", beta, "me", "second", base.Add(-20*time.Minute), true)
	insertMessage("a1", alpha, "111", "first", base.Add(-30*time.Minute), false)
	insertMessage("x1", excluded, "333", "skip me", base.Add(-10*time.Minute), false)

	result, err := db.DigestMessages(DigestOptions{
		After:       base.Add(-1 * time.Hour),
		Before:      base,
		ExcludeJIDs: []string{excluded},
		Limit:       100,
	})
	if err != nil {
		t.Fatalf("digest messages: %v", err)
	}

	if result.MessageCount != 2 {
		t.Fatalf("expected 2 messages, got %d", result.MessageCount)
	}
	if result.ChatCount != 2 {
		t.Fatalf("expected 2 chats, got %d", result.ChatCount)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("expected 2 digest messages, got %d", len(result.Messages))
	}
	if result.Messages[0].ID != "a1" || result.Messages[1].ID != "b2" {
		t.Fatalf("expected ascending messages [a1 b2], got [%s %s]", result.Messages[0].ID, result.Messages[1].ID)
	}
	if result.Messages[0].ChatName == nil || *result.Messages[0].ChatName != "Alpha" {
		t.Fatalf("expected Alpha chat name, got %#v", result.Messages[0].ChatName)
	}
}

func TestDigestMessagesExtractsURLsAndLatestTimestamp(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "messages.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.CloseQuietly()

	jid := "111@s.whatsapp.net"
	_, err = db.Messages.Exec(`INSERT INTO chats (jid, name) VALUES (?, ?)`, jid, "Links")
	if err != nil {
		t.Fatalf("insert chat: %v", err)
	}

	base := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)
	_, err = db.Messages.Exec(`INSERT INTO messages (id, chat_jid, sender, content, timestamp, is_from_me) VALUES (?, ?, ?, ?, ?, ?), (?, ?, ?, ?, ?, ?)`,
		"m1", jid, "111", "see https://example.com/a", base.Add(-2*time.Minute), false,
		"m2", jid, "111", "later https://example.com/b", base.Add(-1*time.Minute), false,
	)
	if err != nil {
		t.Fatalf("insert messages: %v", err)
	}

	result, err := db.DigestMessages(DigestOptions{After: base.Add(-1 * time.Hour), Before: base, Limit: 100})
	if err != nil {
		t.Fatalf("digest messages: %v", err)
	}

	if result.LatestMessageTime == nil || !result.LatestMessageTime.Equal(base.Add(-1*time.Minute)) {
		t.Fatalf("expected latest timestamp %s, got %#v", base.Add(-1*time.Minute), result.LatestMessageTime)
	}
	if len(result.URLs) != 2 || result.URLs[0] != "https://example.com/a" || result.URLs[1] != "https://example.com/b" {
		t.Fatalf("unexpected URLs: %#v", result.URLs)
	}
}
