package parser

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

func TestParse_AssistantWithUsage(t *testing.T) {
	input := `{"type":"assistant","sessionId":"sess-1","timestamp":"2026-03-30T10:00:00Z","uuid":"u1","message":{"role":"assistant","content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":10,"cache_creation_input_tokens":5}}}`

	sessions, err := Parse(strings.NewReader(input), testLogger)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}
	s := sessions[0]
	if s.SessionID != "sess-1" {
		t.Fatalf("want sessionId sess-1, got %q", s.SessionID)
	}
	if len(s.Messages) != 1 {
		t.Fatalf("want 1 message, got %d", len(s.Messages))
	}
	m := s.Messages[0]
	if m.Usage == nil {
		t.Fatal("usage should not be nil")
	}
	if m.Usage.InputTokens != 100 {
		t.Fatalf("want input_tokens 100, got %d", m.Usage.InputTokens)
	}
	if m.Usage.OutputTokens != 50 {
		t.Fatalf("want output_tokens 50, got %d", m.Usage.OutputTokens)
	}
	if m.Usage.CacheReadInputTokens != 10 {
		t.Fatalf("want cache_read 10, got %d", m.Usage.CacheReadInputTokens)
	}
	if m.Usage.CacheCreationInputTokens != 5 {
		t.Fatalf("want cache_creation 5, got %d", m.Usage.CacheCreationInputTokens)
	}
}

func TestParse_ToolUseAndResult(t *testing.T) {
	lines := strings.Join([]string{
		`{"type":"assistant","sessionId":"sess-1","timestamp":"2026-03-30T10:00:00Z","uuid":"u1","message":{"role":"assistant","content":[{"type":"tool_use","id":"tool-1","name":"Read","input":{"path":"/tmp/f"}}],"usage":{"input_tokens":10,"output_tokens":20}}}`,
		`{"type":"user","sessionId":"sess-1","timestamp":"2026-03-30T10:00:01Z","uuid":"u2","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":[{"type":"text","text":"file contents"}],"is_error":false}]}}`,
	}, "\n")

	sessions, err := Parse(strings.NewReader(lines), testLogger)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}
	if len(sessions[0].Messages) != 2 {
		t.Fatalf("want 2 messages, got %d", len(sessions[0].Messages))
	}
	// Check tool_use
	assistantContent := sessions[0].Messages[0].Content
	if len(assistantContent) != 1 || assistantContent[0].Type != "tool_use" {
		t.Fatalf("want tool_use content block, got %+v", assistantContent)
	}
	if assistantContent[0].Name != "Read" {
		t.Fatalf("want tool name Read, got %q", assistantContent[0].Name)
	}
	// Check tool_result
	userContent := sessions[0].Messages[1].Content
	if len(userContent) != 1 || userContent[0].Type != "tool_result" {
		t.Fatalf("want tool_result content block, got %+v", userContent)
	}
	if userContent[0].ToolUseID != "tool-1" {
		t.Fatalf("want tool_use_id tool-1, got %q", userContent[0].ToolUseID)
	}
}

func TestParse_SessionAggregation(t *testing.T) {
	var lines []string
	for i := 0; i < 20; i++ {
		ts := fmt.Sprintf("2026-03-30T10:%02d:00Z", i)
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		content := fmt.Sprintf(`[{"type":"text","text":"msg %d"}]`, i)
		usage := ""
		if role == "assistant" {
			usage = `,"usage":{"input_tokens":1,"output_tokens":1}`
		}
		line := fmt.Sprintf(`{"type":"%s","sessionId":"sess-agg","timestamp":"%s","uuid":"u%d","message":{"role":"%s","content":%s%s}}`, role, ts, i, role, content, usage)
		lines = append(lines, line)
	}

	sessions, err := Parse(strings.NewReader(strings.Join(lines, "\n")), testLogger)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}
	s := sessions[0]
	if len(s.Messages) != 20 {
		t.Fatalf("want 20 messages, got %d", len(s.Messages))
	}
	if s.SessionID != "sess-agg" {
		t.Fatalf("want sessionId sess-agg, got %q", s.SessionID)
	}
	// StartTime = first message, EndTime = last message
	if !s.StartTime.Equal(s.Messages[0].Timestamp) {
		t.Fatalf("StartTime should match first message timestamp")
	}
	if !s.EndTime.Equal(s.Messages[len(s.Messages)-1].Timestamp) {
		t.Fatalf("EndTime should match last message timestamp")
	}
}

func TestParse_MalformedLineSkipped(t *testing.T) {
	lines := strings.Join([]string{
		`{"type":"assistant","sessionId":"sess-1","timestamp":"2026-03-30T10:00:00Z","uuid":"u1","message":{"role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}}`,
		`{this is not valid json`,
		`{"type":"assistant","sessionId":"sess-1","timestamp":"2026-03-30T10:01:00Z","uuid":"u2","message":{"role":"assistant","content":[{"type":"text","text":"still ok"}],"usage":{"input_tokens":2,"output_tokens":2}}}`,
	}, "\n")

	sessions, err := Parse(strings.NewReader(lines), testLogger)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}
	if len(sessions[0].Messages) != 2 {
		t.Fatalf("want 2 messages (skipping malformed), got %d", len(sessions[0].Messages))
	}
}

func TestParse_EmptyFile(t *testing.T) {
	sessions, err := Parse(strings.NewReader(""), testLogger)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("want 0 sessions for empty file, got %d", len(sessions))
	}
}

func TestParse_SkipsNonMessageTypes(t *testing.T) {
	lines := strings.Join([]string{
		`{"type":"progress","sessionId":"sess-1","timestamp":"2026-03-30T10:00:00Z","uuid":"u0","data":{"type":"hook_progress"}}`,
		`{"type":"assistant","sessionId":"sess-1","timestamp":"2026-03-30T10:00:01Z","uuid":"u1","message":{"role":"assistant","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":1,"output_tokens":1}}}`,
		`{"type":"system","sessionId":"sess-1","timestamp":"2026-03-30T10:00:02Z","uuid":"u2","subtype":"bridge_status"}`,
		`{"type":"file-history-snapshot","messageId":"m1","snapshot":{}}`,
	}, "\n")

	sessions, err := Parse(strings.NewReader(lines), testLogger)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}
	if len(sessions[0].Messages) != 1 {
		t.Fatalf("want 1 message (only assistant), got %d", len(sessions[0].Messages))
	}
}

func TestParse_UserMessageStringContent(t *testing.T) {
	input := `{"type":"user","sessionId":"sess-1","timestamp":"2026-03-30T10:00:00Z","uuid":"u1","message":{"role":"user","content":"hello world"}}`

	sessions, err := Parse(strings.NewReader(input), testLogger)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}
	if len(sessions[0].Messages) != 1 {
		t.Fatalf("want 1 message, got %d", len(sessions[0].Messages))
	}
	m := sessions[0].Messages[0]
	if len(m.Content) != 1 || m.Content[0].Type != "text" || m.Content[0].Text != "hello world" {
		t.Fatalf("want text content block with 'hello world', got %+v", m.Content)
	}
}
