package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type MonthCoverage struct {
	Month    string `json:"month"`
	Messages int    `json:"messages"`
	Chats    int    `json:"chats"`
}

type ChatCoverage struct {
	ChatJID      string     `json:"chat_jid"`
	ChatName     string     `json:"chat_name,omitempty"`
	IsGroup      bool       `json:"is_group"`
	Messages     int        `json:"messages"`
	FirstSeen    *time.Time `json:"first_seen,omitempty"`
	LastSeen     *time.Time `json:"last_seen,omitempty"`
	ActiveMonths int        `json:"active_months"`
}

type CoverageReport struct {
	TotalMessages int             `json:"total_messages"`
	TotalChats    int             `json:"total_chats"`
	FirstMessage  *time.Time      `json:"first_message,omitempty"`
	LastMessage   *time.Time      `json:"last_message,omitempty"`
	Months        []MonthCoverage `json:"months"`
	Chats         []ChatCoverage  `json:"chats"`
}

type IntegrityResult struct {
	OK       bool     `json:"ok"`
	Messages []string `json:"messages"`
}

type MaintenanceResult struct {
	Action string `json:"action"`
	OK     bool   `json:"ok"`
	Path   string `json:"path,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (d *DB) CoverageReport(chatLimit int) (CoverageReport, error) {
	if chatLimit <= 0 {
		chatLimit = 25
	}
	report := CoverageReport{}
	if err := d.Messages.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&report.TotalMessages); err != nil {
		return report, err
	}
	if err := d.Messages.QueryRow(`SELECT COUNT(*) FROM chats`).Scan(&report.TotalChats); err != nil {
		return report, err
	}
	first, err := scanNullableTime(d.Messages.QueryRow(`SELECT MIN(timestamp) FROM messages`))
	if err != nil {
		return report, err
	}
	last, err := scanNullableTime(d.Messages.QueryRow(`SELECT MAX(timestamp) FROM messages`))
	if err != nil {
		return report, err
	}
	report.FirstMessage = first
	report.LastMessage = last

	monthRows, err := d.Messages.Query(`
		SELECT substr(timestamp, 1, 7) AS month, COUNT(*) AS messages, COUNT(DISTINCT chat_jid) AS chats
		FROM messages
		WHERE timestamp IS NOT NULL AND timestamp != ''
		GROUP BY month
		ORDER BY month ASC
	`)
	if err != nil {
		return report, err
	}
	defer func() { _ = monthRows.Close() }()
	for monthRows.Next() {
		var row MonthCoverage
		if err := monthRows.Scan(&row.Month, &row.Messages, &row.Chats); err != nil {
			return report, err
		}
		report.Months = append(report.Months, row)
	}
	if err := monthRows.Err(); err != nil {
		return report, err
	}

	chatRows, err := d.Messages.Query(`
		SELECT m.chat_jid, COALESCE(c.name, ''), COUNT(*) AS messages,
		       MIN(m.timestamp), MAX(m.timestamp), COUNT(DISTINCT substr(m.timestamp, 1, 7)) AS active_months
		FROM messages m
		LEFT JOIN chats c ON c.jid = m.chat_jid
		GROUP BY m.chat_jid
		ORDER BY messages DESC, MAX(m.timestamp) DESC
		LIMIT ?
	`, chatLimit)
	if err != nil {
		return report, err
	}
	defer func() { _ = chatRows.Close() }()
	for chatRows.Next() {
		var row ChatCoverage
		var firstRaw, lastRaw sql.NullString
		if err := chatRows.Scan(&row.ChatJID, &row.ChatName, &row.Messages, &firstRaw, &lastRaw, &row.ActiveMonths); err != nil {
			return report, err
		}
		row.IsGroup = strings.HasSuffix(row.ChatJID, "@g.us")
		row.FirstSeen = parseNullableTime(firstRaw)
		row.LastSeen = parseNullableTime(lastRaw)
		report.Chats = append(report.Chats, row)
	}
	return report, chatRows.Err()
}

func (d *DB) IntegrityCheck() (IntegrityResult, error) {
	rows, err := d.Messages.Query(`PRAGMA integrity_check`)
	if err != nil {
		return IntegrityResult{}, err
	}
	defer func() { _ = rows.Close() }()
	result := IntegrityResult{OK: true}
	for rows.Next() {
		var msg string
		if err := rows.Scan(&msg); err != nil {
			return result, err
		}
		result.Messages = append(result.Messages, msg)
		if msg != "ok" {
			result.OK = false
		}
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	if len(result.Messages) == 0 {
		result.OK = false
	}
	return result, nil
}

func (d *DB) RebuildFTS() error {
	_, err := d.Messages.Exec(`INSERT INTO messages_fts(messages_fts) VALUES('rebuild')`)
	return err
}

func (d *DB) Vacuum() error {
	_, err := d.Messages.Exec(`VACUUM`)
	return err
}

func BackupDatabase(dbPath, backupDir string) (string, error) {
	if backupDir == "" {
		backupDir = filepath.Join(filepath.Dir(filepath.Dir(dbPath)), "backups")
	}
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return "", err
	}
	stamp := time.Now().Format("20060102-150405")
	dst := filepath.Join(backupDir, fmt.Sprintf("messages-%s.db", stamp))
	data, err := os.ReadFile(dbPath)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, data, 0600); err != nil {
		return "", err
	}
	return dst, nil
}

func scanNullableTime(row *sql.Row) (*time.Time, error) {
	var raw sql.NullString
	if err := row.Scan(&raw); err != nil {
		return nil, err
	}
	return parseNullableTime(raw), nil
}

func parseNullableTime(raw sql.NullString) *time.Time {
	if !raw.Valid || raw.String == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05-07:00", "2006-01-02 15:04:05Z07:00"} {
		if ts, err := time.Parse(layout, raw.String); err == nil {
			return &ts
		}
	}
	return nil
}
