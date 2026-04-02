package events

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/tim80411/claude-code-otel-exporter/internal/parser"
)

// Event represents a structured log event to push to Loki.
type Event struct {
	Timestamp time.Time
	Body      map[string]interface{}
}

type toolUseInfo struct {
	Name      string
	Input     json.RawMessage
	Timestamp time.Time
}

// ExtractEvents extracts all log events from a session.
// Returns tool_result, api_request, api_error, and user_prompt events.
func ExtractEvents(sess parser.Session) []Event {
	var events []Event

	// Collect tool_use info indexed by ID for linking.
	toolUses := make(map[string]toolUseInfo)

	for _, msg := range sess.Messages {
		for _, block := range msg.Content {
			if block.Type == "tool_use" {
				toolUses[block.ID] = toolUseInfo{
					Name:      block.Name,
					Input:     block.Input,
					Timestamp: msg.Timestamp,
				}
			}
		}
	}

	for i, msg := range sess.Messages {
		for _, block := range msg.Content {
			switch block.Type {
			case "tool_result":
				ev := buildToolResultEvent(block, toolUses, msg.Timestamp)
				if ev != nil {
					events = append(events, *ev)
				}
			}
		}

		// API request / error events from assistant messages.
		if msg.Role == "assistant" && msg.Usage != nil {
			ev := buildAPIRequestEvent(msg, sess.Messages, i)
			events = append(events, ev)
		}

		// User prompt events.
		if msg.Role == "user" && hasTextContent(msg) {
			events = append(events, Event{
				Timestamp: msg.Timestamp,
				Body:      map[string]interface{}{"event": "claude_code.user_prompt"},
			})
		}
	}

	return events
}

func buildToolResultEvent(block parser.ContentBlock, toolUses map[string]toolUseInfo, resultTime time.Time) *Event {
	tu, ok := toolUses[block.ToolUseID]
	if !ok {
		return nil
	}

	body := map[string]interface{}{
		"event":     "claude_code.tool_result",
		"tool_name": tu.Name,
		"success":   fmt.Sprintf("%t", !block.IsError),
	}

	duration := resultTime.Sub(tu.Timestamp)
	if duration >= 0 {
		body["duration_ms"] = duration.Milliseconds()
	}

	if block.IsError {
		body["error"] = extractErrorText(block)
	} else {
		body["error"] = ""
	}

	return &Event{Timestamp: resultTime, Body: body}
}

func buildAPIRequestEvent(msg parser.Message, messages []parser.Message, idx int) Event {
	body := map[string]interface{}{
		"model":        msg.Model,
		"input_tokens": msg.Usage.InputTokens,
		"output_tokens": msg.Usage.OutputTokens,
	}

	// Estimate duration from previous message timestamp.
	if idx > 0 {
		prev := messages[idx-1]
		duration := msg.Timestamp.Sub(prev.Timestamp)
		if duration >= 0 {
			body["duration_ms"] = duration.Milliseconds()
		}
	}

	// Check if this looks like an error response (e.g., overloaded, rate limit).
	if isAPIError(msg) {
		body["event"] = "claude_code.api_error"
		body["status_code"] = guessStatusCode(msg)
		body["error"] = extractAPIError(msg)
	} else {
		body["event"] = "claude_code.api_request"
	}

	return Event{Timestamp: msg.Timestamp, Body: body}
}

func hasTextContent(msg parser.Message) bool {
	for _, block := range msg.Content {
		if block.Type == "text" && block.Text != "" {
			return true
		}
	}
	return false
}

func isAPIError(msg parser.Message) bool {
	for _, block := range msg.Content {
		if block.Type == "text" {
			// Check for common API error patterns in the response text.
			if containsErrorPattern(block.Text) {
				return true
			}
		}
	}
	return false
}

func containsErrorPattern(text string) bool {
	patterns := []string{"overloaded_error", "rate_limit_error", "api_error", "internal_server_error"}
	for _, p := range patterns {
		if len(text) > 0 && containsSubstring(text, p) {
			return true
		}
	}
	return false
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func guessStatusCode(msg parser.Message) string {
	for _, block := range msg.Content {
		if block.Type == "text" {
			if containsSubstring(block.Text, "rate_limit") {
				return "429"
			}
			if containsSubstring(block.Text, "overloaded") {
				return "529"
			}
			if containsSubstring(block.Text, "internal_server_error") {
				return "500"
			}
		}
	}
	return "unknown"
}

func extractAPIError(msg parser.Message) string {
	for _, block := range msg.Content {
		if block.Type == "text" && containsErrorPattern(block.Text) {
			if len(block.Text) > 200 {
				return block.Text[:200]
			}
			return block.Text
		}
	}
	return ""
}

func extractErrorText(block parser.ContentBlock) string {
	if len(block.Content) == 0 {
		return ""
	}
	// Try to parse content as string.
	var s string
	if err := json.Unmarshal(block.Content, &s); err == nil {
		return s
	}
	// Try as array of content blocks.
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(block.Content, &blocks); err == nil {
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				return b.Text
			}
		}
	}
	return ""
}
