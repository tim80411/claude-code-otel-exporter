package parser

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"time"
)

// Parse reads JSONL lines from r, parses each into Messages,
// and aggregates them into Sessions grouped by sessionId.
// Malformed lines are skipped with a warning log.
func Parse(r io.Reader, logger *slog.Logger) ([]Session, error) {
	sessionMap := make(map[string]*Session)

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024) // up to 10MB per line
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var raw RawLine
		if err := json.Unmarshal(line, &raw); err != nil {
			logger.Warn("skipping malformed line", "line", lineNum, "error", err)
			continue
		}

		// Only process lines that have a message (assistant/user with message payload).
		if raw.Message == nil {
			continue
		}
		if raw.Type != "assistant" && raw.Type != "user" {
			continue
		}
		if raw.SessionID == "" {
			continue
		}

		msg, err := parseMessage(raw)
		if err != nil {
			logger.Warn("skipping unparseable message", "line", lineNum, "error", err)
			continue
		}

		sess, ok := sessionMap[raw.SessionID]
		if !ok {
			sess = &Session{SessionID: raw.SessionID}
			sessionMap[raw.SessionID] = sess
		}
		sess.Messages = append(sess.Messages, msg)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parser: scan: %w", err)
	}

	if len(sessionMap) == 0 {
		return nil, nil
	}

	// Convert map to sorted slice, compute start/end times.
	sessions := make([]Session, 0, len(sessionMap))
	for _, sess := range sessionMap {
		if len(sess.Messages) > 0 {
			sess.StartTime = sess.Messages[0].Timestamp
			sess.EndTime = sess.Messages[len(sess.Messages)-1].Timestamp
		}
		sessions = append(sessions, *sess)
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartTime.Before(sessions[j].StartTime)
	})
	return sessions, nil
}

func parseMessage(raw RawLine) (Message, error) {
	ts, err := time.Parse(time.RFC3339, raw.Timestamp)
	if err != nil {
		return Message{}, fmt.Errorf("parse timestamp %q: %w", raw.Timestamp, err)
	}

	content, err := parseContent(raw.Message.Content)
	if err != nil {
		return Message{}, err
	}

	return Message{
		Role:      raw.Message.Role,
		Timestamp: ts,
		UUID:      raw.UUID,
		Model:     raw.Message.Model,
		Usage:     raw.Message.Usage,
		Content:   content,
	}, nil
}

// parseContent handles both string content (user text) and array content ([]ContentBlock).
func parseContent(raw json.RawMessage) ([]ContentBlock, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	// Try as string first (user plain text messages).
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return []ContentBlock{{Type: "text", Text: str}}, nil
	}

	// Try as array of content blocks.
	var blocks []ContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, fmt.Errorf("parse content: %w", err)
	}
	return blocks, nil
}
