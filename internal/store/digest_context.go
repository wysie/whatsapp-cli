package store

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const defaultDigestSummaryMaxChars = 30000

type digestContextLine struct {
	text  string
	score int
	idx   int
}

// BuildDigestSummaryContext renders a bounded, deterministic text context for LLM summaries.
// It preserves high-signal items (URLs, action/event-looking messages, recent messages)
// before low-value chatter so cron prompts stay small and reliable.
func BuildDigestSummaryContext(result DigestResult, maxChars int) string {
	if maxChars <= 0 {
		maxChars = defaultDigestSummaryMaxChars
	}
	if maxChars < 200 {
		maxChars = 200
	}

	chatCounts := make(map[string]int)
	chatNames := make(map[string]string)
	for _, msg := range result.Messages {
		chatCounts[msg.ChatJID]++
		chatNames[msg.ChatJID] = messageChatName(msg)
	}

	var b strings.Builder
	appendLine := func(format string, args ...any) {
		_, _ = fmt.Fprintf(&b, format, args...)
		b.WriteByte('\n')
	}

	appendLine("SUMMARY_CONTEXT")
	appendLine("WINDOW: %s -> %s", result.After.Format(time.RFC3339), result.Before.Format(time.RFC3339))
	appendLine("RAW_MESSAGES: %d", result.MessageCount)
	appendLine("CHAT_COUNT: %d", result.ChatCount)
	if result.LatestMessageTime != nil {
		appendLine("LATEST_MESSAGE_TIME: %s", result.LatestMessageTime.Format(time.RFC3339))
	}
	appendLine("")

	appendLine("ACTIVE_CHATS:")
	type chatCount struct {
		jid   string
		name  string
		count int
	}
	chats := make([]chatCount, 0, len(chatCounts))
	for jid, count := range chatCounts {
		chats = append(chats, chatCount{jid: jid, name: chatNames[jid], count: count})
	}
	sort.Slice(chats, func(i, j int) bool {
		if chats[i].count == chats[j].count {
			return chats[i].name < chats[j].name
		}
		return chats[i].count > chats[j].count
	})
	for _, chat := range chats {
		appendLine("- %s (%s): %d messages", chat.name, chat.jid, chat.count)
	}
	appendLine("")

	if len(result.URLs) > 0 {
		appendLine("URLS:")
		for _, url := range result.URLs {
			appendLine("- %s", url)
		}
		appendLine("")
	}

	header := b.String()
	lines := rankedDigestMessageLines(result.Messages)
	selected := make([]digestContextLine, 0, len(lines))
	used := len(header) + len("MESSAGES:\n")
	for _, line := range lines {
		lineLen := len(line.text) + 1
		if used+lineLen > maxChars {
			continue
		}
		selected = append(selected, line)
		used += lineLen
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].idx < selected[j].idx })

	var out strings.Builder
	out.WriteString(header)
	out.WriteString("MESSAGES:\n")
	for _, line := range selected {
		out.WriteString(line.text)
		out.WriteByte('\n')
	}
	if dropped := len(lines) - len(selected); dropped > 0 {
		marker := fmt.Sprintf("TRUNCATED_MESSAGES: %d lower-priority messages omitted\n", dropped)
		if out.Len()+len(marker) <= maxChars {
			out.WriteString(marker)
		} else if maxChars > len(marker) {
			text := out.String()
			text = text[:maxChars-len(marker)]
			if idx := strings.LastIndex(text, "\n"); idx > 0 && idx > len(header) {
				text = text[:idx+1]
			}
			out.Reset()
			out.WriteString(strings.TrimRight(text, "\n "))
			out.WriteByte('\n')
			out.WriteString(marker)
		}
	}

	text := out.String()
	if len(text) > maxChars {
		text = text[:maxChars]
		if idx := strings.LastIndex(text, "\n"); idx > 0 && idx > maxChars-500 {
			text = text[:idx+1]
		}
	}
	return text
}

func rankedDigestMessageLines(messages []Message) []digestContextLine {
	lines := make([]digestContextLine, 0, len(messages))
	for i, msg := range messages {
		content := messageContent(msg)
		if content == "" {
			continue
		}
		line := fmt.Sprintf("[%s] [%s] %s: %s", msg.Timestamp.Format(time.RFC3339), messageChatName(msg), messageSenderName(msg), content)
		lines = append(lines, digestContextLine{text: line, score: digestMessageScore(msg, content, i, len(messages)), idx: i})
	}
	sort.SliceStable(lines, func(i, j int) bool {
		if lines[i].score == lines[j].score {
			return lines[i].idx > lines[j].idx
		}
		return lines[i].score > lines[j].score
	})
	return lines
}

func digestMessageScore(msg Message, content string, idx, total int) int {
	score := 0
	lower := strings.ToLower(content)
	if digestURLPattern.MatchString(content) {
		score += 100
	}
	for _, kw := range []string{"please", "confirm", "pay", "paid", "due", "by ", "deadline", "book", "settle", "urgent", "need", "todo", "action"} {
		if strings.Contains(lower, kw) {
			score += 20
		}
	}
	for _, kw := range []string{"today", "tomorrow", "tonight", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday", "am", "pm", "meeting", "game", "appointment", "event"} {
		if strings.Contains(lower, kw) {
			score += 15
		}
	}
	if msg.IsFromMe {
		score += 8
	}
	if len(content) > 20 {
		score += 5
	}
	// Mild recency boost while preserving high-signal older lines.
	if total > 0 {
		score += (idx * 10) / total
	}
	return score
}

func messageChatName(msg Message) string {
	if msg.ChatName != nil && strings.TrimSpace(*msg.ChatName) != "" {
		return strings.TrimSpace(*msg.ChatName)
	}
	return msg.ChatJID
}

func messageSenderName(msg Message) string {
	if msg.IsFromMe {
		return "You"
	}
	if msg.SenderName != nil && strings.TrimSpace(*msg.SenderName) != "" {
		return strings.TrimSpace(*msg.SenderName)
	}
	return msg.Sender
}

func messageContent(msg Message) string {
	content := ""
	if msg.Content != nil {
		content = strings.Join(strings.Fields(*msg.Content), " ")
	}
	if content == "" && msg.MediaType != nil && *msg.MediaType != "" {
		content = "[" + *msg.MediaType + "]"
	}
	return content
}
