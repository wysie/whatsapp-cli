package store

import (
	"path/filepath"
	"testing"
	"time"
)

func openAnalyticsTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "messages.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(db.CloseQuietly)
	return db
}

func insertAnalyticsChat(t *testing.T, db *DB, jid, name string, isGroup bool) {
	t.Helper()
	_, err := db.Messages.Exec(`INSERT INTO chats(jid, name, last_message_time) VALUES(?, ?, ?)`, jid, name, time.Now().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insert chat: %v", err)
	}
}

func insertAnalyticsMessage(t *testing.T, db *DB, id, chatJID, ts string) {
	t.Helper()
	_, err := db.Messages.Exec(`INSERT INTO messages(id, chat_jid, sender, content, timestamp, is_from_me) VALUES(?, ?, ?, ?, ?, 0)`, id, chatJID, "sender", "hello", ts)
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
}

func TestCoverageReportGroupsMessagesByMonthAndChat(t *testing.T) {
	db := openAnalyticsTestDB(t)
	insertAnalyticsChat(t, db, "a@g.us", "Alpha", true)
	insertAnalyticsChat(t, db, "b@s.whatsapp.net", "Beta", false)
	insertAnalyticsMessage(t, db, "m1", "a@g.us", "2026-01-02T10:00:00+08:00")
	insertAnalyticsMessage(t, db, "m2", "a@g.us", "2026-01-03T10:00:00+08:00")
	insertAnalyticsMessage(t, db, "m3", "b@s.whatsapp.net", "2026-02-04T10:00:00+08:00")

	report, err := db.CoverageReport(10)
	if err != nil {
		t.Fatalf("coverage report: %v", err)
	}
	if report.TotalMessages != 3 || report.TotalChats != 2 {
		t.Fatalf("unexpected totals: %+v", report)
	}
	if len(report.Months) != 2 || report.Months[0].Month != "2026-01" || report.Months[0].Messages != 2 || report.Months[1].Month != "2026-02" {
		t.Fatalf("unexpected month buckets: %+v", report.Months)
	}
	if len(report.Chats) != 2 || report.Chats[0].ChatName != "Alpha" || report.Chats[0].Messages != 2 || report.Chats[1].ChatName != "Beta" {
		t.Fatalf("unexpected chat coverage: %+v", report.Chats)
	}
}

func TestMaintenanceIntegrityCheckAndFTSRebuild(t *testing.T) {
	db := openAnalyticsTestDB(t)
	insertAnalyticsChat(t, db, "a@g.us", "Alpha", true)
	insertAnalyticsMessage(t, db, "m1", "a@g.us", "2026-01-02T10:00:00+08:00")

	integrity, err := db.IntegrityCheck()
	if err != nil {
		t.Fatalf("integrity: %v", err)
	}
	if !integrity.OK || len(integrity.Messages) == 0 || integrity.Messages[0] != "ok" {
		t.Fatalf("unexpected integrity result: %+v", integrity)
	}
	if err := db.RebuildFTS(); err != nil {
		t.Fatalf("rebuild fts: %v", err)
	}
}
