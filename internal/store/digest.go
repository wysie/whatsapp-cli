package store

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var digestURLPattern = regexp.MustCompile(`https?://[^\s<>()"']+`)

// DigestOptions controls deterministic local message extraction for summaries.
type DigestOptions struct {
	After       time.Time
	Before      time.Time
	ExcludeJIDs []string
	Limit       int
}

// DigestResult is an agent-friendly deterministic summary input payload.
type DigestResult struct {
	After             time.Time  `json:"after"`
	Before            time.Time  `json:"before"`
	MessageCount      int        `json:"message_count"`
	ChatCount         int        `json:"chat_count"`
	LatestMessageTime *time.Time `json:"latest_message_time,omitempty"`
	URLs              []string   `json:"urls,omitempty"`
	Messages          []Message  `json:"messages"`
}

// DigestMessages returns messages in chronological order for a summary window.
func (d *DB) DigestMessages(opts DigestOptions) (DigestResult, error) {
	if opts.Limit <= 0 {
		opts.Limit = 5000
	}

	query := `
		SELECT m.id, m.chat_jid, m.sender,
		       COALESCE(m.sender_name, l.name) as sender_name,
		       m.content, m.timestamp, m.is_from_me,
		       m.media_type, m.filename, c.name as chat_name
		FROM messages m
		LEFT JOIN chats c ON m.chat_jid = c.jid
		LEFT JOIN lid_mappings l ON m.sender = l.lid
		WHERE 1=1
	`
	var args []any

	if !opts.After.IsZero() {
		query += " AND m.timestamp >= ?"
		args = append(args, opts.After)
	}
	if !opts.Before.IsZero() {
		query += " AND m.timestamp <= ?"
		args = append(args, opts.Before)
	}
	if len(opts.ExcludeJIDs) > 0 {
		placeholders := make([]string, 0, len(opts.ExcludeJIDs))
		for _, jid := range opts.ExcludeJIDs {
			jid = strings.TrimSpace(jid)
			if jid == "" {
				continue
			}
			placeholders = append(placeholders, "?")
			args = append(args, jid)
		}
		if len(placeholders) > 0 {
			query += fmt.Sprintf(" AND m.chat_jid NOT IN (%s)", strings.Join(placeholders, ","))
		}
	}

	query += " ORDER BY m.timestamp ASC"
	query += fmt.Sprintf(" LIMIT %d", opts.Limit)

	messages, err := d.scanMessages(query, args)
	if err != nil {
		return DigestResult{}, err
	}

	chatSet := make(map[string]struct{})
	urlSeen := make(map[string]struct{})
	var urls []string
	var latest *time.Time
	for _, msg := range messages {
		chatSet[msg.ChatJID] = struct{}{}
		if latest == nil || msg.Timestamp.After(*latest) {
			t := msg.Timestamp
			latest = &t
		}
		if msg.Content != nil {
			for _, rawURL := range digestURLPattern.FindAllString(*msg.Content, -1) {
				url := strings.TrimRight(rawURL, ".,;:!?)]}")
				if _, ok := urlSeen[url]; ok {
					continue
				}
				urlSeen[url] = struct{}{}
				urls = append(urls, url)
			}
		}
	}

	return DigestResult{
		After:             opts.After,
		Before:            opts.Before,
		MessageCount:      len(messages),
		ChatCount:         len(chatSet),
		LatestMessageTime: latest,
		URLs:              urls,
		Messages:          messages,
	}, nil
}
