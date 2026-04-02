package events

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/tim80411/claude-code-otel-exporter/internal/parser"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func ts(offset time.Duration) time.Time { return t0.Add(offset) }

func makeToolUse(id, name string, input interface{}) parser.ContentBlock {
	raw, _ := json.Marshal(input)
	return parser.ContentBlock{Type: "tool_use", ID: id, Name: name, Input: raw}
}

func makeToolResult(toolUseID string, isError bool) parser.ContentBlock {
	return parser.ContentBlock{Type: "tool_result", ToolUseID: toolUseID, IsError: isError}
}

func findEventByName(events []Event, name string) *Event {
	for i, ev := range events {
		if ev.Body["event"] == name {
			return &events[i]
		}
	}
	return nil
}

func countEventsByName(events []Event, name string) int {
	count := 0
	for _, ev := range events {
		if ev.Body["event"] == name {
			count++
		}
	}
	return count
}

func TestExtract_ToolResultEvent(t *testing.T) {
	sess := parser.Session{
		SessionID: "s1",
		Messages: []parser.Message{
			{
				Role:      "assistant",
				Timestamp: ts(0),
				Content: []parser.ContentBlock{
					makeToolUse("t1", "Read", map[string]string{"path": "/tmp/f"}),
				},
			},
			{
				Role:      "user",
				Timestamp: ts(42 * time.Millisecond),
				Content:   []parser.ContentBlock{makeToolResult("t1", false)},
			},
		},
	}

	evts := ExtractEvents(sess)
	ev := findEventByName(evts, "claude_code.tool_result")
	if ev == nil {
		t.Fatal("expected tool_result event")
	}
	if ev.Body["tool_name"] != "Read" {
		t.Errorf("tool_name = %v, want Read", ev.Body["tool_name"])
	}
	if ev.Body["success"] != "true" {
		t.Errorf("success = %v, want true", ev.Body["success"])
	}
	if ev.Body["duration_ms"] != int64(42) {
		t.Errorf("duration_ms = %v, want 42", ev.Body["duration_ms"])
	}
}

func TestExtract_ToolResultError(t *testing.T) {
	sess := parser.Session{
		SessionID: "s1",
		Messages: []parser.Message{
			{
				Role:      "assistant",
				Timestamp: ts(0),
				Content: []parser.ContentBlock{
					makeToolUse("t1", "Bash", map[string]string{"command": "rm -rf /"}),
				},
			},
			{
				Role:      "user",
				Timestamp: ts(100 * time.Millisecond),
				Content:   []parser.ContentBlock{makeToolResult("t1", true)},
			},
		},
	}

	evts := ExtractEvents(sess)
	ev := findEventByName(evts, "claude_code.tool_result")
	if ev == nil {
		t.Fatal("expected tool_result event")
	}
	if ev.Body["success"] != "false" {
		t.Errorf("success = %v, want false", ev.Body["success"])
	}
}

func TestExtract_APIRequestEvent(t *testing.T) {
	sess := parser.Session{
		SessionID: "s1",
		Messages: []parser.Message{
			{
				Role:      "user",
				Timestamp: ts(0),
				Content:   []parser.ContentBlock{{Type: "text", Text: "hello"}},
			},
			{
				Role:      "assistant",
				Timestamp: ts(1500 * time.Millisecond),
				Model:     "claude-opus-4-6",
				Usage:     &parser.Usage{InputTokens: 5000, OutputTokens: 1200},
				Content:   []parser.ContentBlock{{Type: "text", Text: "hi there"}},
			},
		},
	}

	evts := ExtractEvents(sess)
	ev := findEventByName(evts, "claude_code.api_request")
	if ev == nil {
		t.Fatal("expected api_request event")
	}
	if ev.Body["model"] != "claude-opus-4-6" {
		t.Errorf("model = %v, want claude-opus-4-6", ev.Body["model"])
	}
	if ev.Body["duration_ms"] != int64(1500) {
		t.Errorf("duration_ms = %v, want 1500", ev.Body["duration_ms"])
	}
	if ev.Body["input_tokens"] != 5000 {
		t.Errorf("input_tokens = %v, want 5000", ev.Body["input_tokens"])
	}
}

func TestExtract_UserPromptEvent(t *testing.T) {
	sess := parser.Session{
		SessionID: "s1",
		Messages: []parser.Message{
			{
				Role:      "user",
				Timestamp: ts(0),
				Content:   []parser.ContentBlock{{Type: "text", Text: "fix the bug"}},
			},
		},
	}

	evts := ExtractEvents(sess)
	ev := findEventByName(evts, "claude_code.user_prompt")
	if ev == nil {
		t.Fatal("expected user_prompt event")
	}
}

func TestExtract_NoEvents(t *testing.T) {
	sess := parser.Session{SessionID: "s1"}
	evts := ExtractEvents(sess)
	if len(evts) != 0 {
		t.Errorf("expected 0 events, got %d", len(evts))
	}
}

func TestExtract_MultipleToolResults(t *testing.T) {
	sess := parser.Session{
		SessionID: "s1",
		Messages: []parser.Message{
			{
				Role:      "assistant",
				Timestamp: ts(0),
				Content: []parser.ContentBlock{
					makeToolUse("t1", "Read", map[string]string{"path": "/a"}),
					makeToolUse("t2", "Write", map[string]string{"file_path": "/b", "content": "x"}),
				},
			},
			{
				Role:      "user",
				Timestamp: ts(50 * time.Millisecond),
				Content: []parser.ContentBlock{
					makeToolResult("t1", false),
					makeToolResult("t2", false),
				},
			},
		},
	}

	evts := ExtractEvents(sess)
	count := countEventsByName(evts, "claude_code.tool_result")
	if count != 2 {
		t.Errorf("tool_result count = %d, want 2", count)
	}
}
