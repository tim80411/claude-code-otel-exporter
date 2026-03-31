# JSONL Parser Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a JSONL parser that reads session files line-by-line, deserializes each line into Go structs, and aggregates messages by sessionId into `Session` objects for downstream metric computation.

**Architecture:** A `parser` package under `internal/parser/` with three files: types (Go structs matching JSONL schema), parser logic (line-by-line parsing + session aggregation), and tests. The parser receives an `io.Reader` (not a file path), making it testable with in-memory data. It returns `[]Session`, skipping malformed lines with warnings.

**Tech Stack:** Go stdlib only — `encoding/json`, `bufio`, `log/slog`, `time`

---

## File Structure

| File | Responsibility |
|------|----------------|
| `internal/parser/types.go` | Go structs: `RawLine`, `Message`, `Usage`, `ContentBlock`, `ToolUseBlock`, `ToolResultBlock`, `Session` |
| `internal/parser/parser.go` | `Parse(r io.Reader, logger) ([]Session, error)` — line-by-line parse + session aggregation |
| `internal/parser/parser_test.go` | All tests: happy path, tool_use, aggregation, malformed lines, empty file |
| `cmd/exporter/main.go` | Wire parser into `runPipeline` — parse each file from reader.Scan results |

## JSONL Schema Reference (from real data)

Every line is a JSON object. Relevant top-level fields:

```
type        string   — "assistant", "user", "system", "progress", "file-history-snapshot"
sessionId   string   — present on all types except "file-history-snapshot"
timestamp   string   — ISO 8601
uuid        string
parentUuid  string|null
message     object   — present on "assistant" and "user" types
```

**Assistant message.content blocks** — array of: `{"type":"text", "text":"..."}`, `{"type":"tool_use", "id":"...", "name":"...", "input":{...}}`, `{"type":"thinking", "thinking":"..."}`

**Assistant message.usage**: `{input_tokens, output_tokens, cache_read_input_tokens, cache_creation_input_tokens}`

**User message with tool_result** — `message.content` is an array containing: `{"type":"tool_result", "tool_use_id":"...", "content":[...], "is_error": bool}`

---

### Task 1: Define Go structs (`types.go`)

**Files:**
- Create: `internal/parser/types.go`

- [ ] **Step 1: Create types.go with all structs**

```go
package parser

import "time"

// RawLine is the top-level structure of every JSONL line.
type RawLine struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionId"`
	Timestamp string          `json:"timestamp"`
	UUID      string          `json:"uuid"`
	Message   *MessagePayload `json:"message,omitempty"`
}

// MessagePayload is the "message" field on assistant/user lines.
type MessagePayload struct {
	Role    string         `json:"role"`
	Content json.RawMessage `json:"content"` // string (user text) or []ContentBlock
	Usage   *Usage         `json:"usage,omitempty"`
	Model   string         `json:"model,omitempty"`
}

// Usage holds token counts from assistant messages.
type Usage struct {
	InputTokens                int `json:"input_tokens"`
	OutputTokens               int `json:"output_tokens"`
	CacheReadInputTokens       int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens   int `json:"cache_creation_input_tokens"`
}

// ContentBlock is one element of the content array.
type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	// tool_use fields
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	// tool_result fields
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

// Message is a parsed, type-safe representation of one JSONL line that has a message.
type Message struct {
	Role      string
	Timestamp time.Time
	UUID      string
	Usage     *Usage
	Content   []ContentBlock
}

// Session aggregates all messages belonging to the same sessionId.
type Session struct {
	SessionID string
	Messages  []Message
	StartTime time.Time
	EndTime   time.Time
}
```

Note: `MessagePayload.Content` is `json.RawMessage` because user messages can have `content` as a plain string or as `[]ContentBlock`. We handle this in the parser.

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/parser/`
Expected: success (no output)

- [ ] **Step 3: Commit**

```bash
git add internal/parser/types.go
git commit -m "feat(parser): add JSONL struct definitions"
```

---

### Task 2: Write failing tests for single-line parsing

**Files:**
- Create: `internal/parser/parser_test.go`
- Create: `internal/parser/parser.go` (stub)

- [ ] **Step 1: Create parser.go stub**

```go
package parser

import (
	"io"
	"log/slog"
)

// Parse reads JSONL lines from r, parses each into Messages,
// and aggregates them into Sessions grouped by sessionId.
// Malformed lines are skipped with a warning log.
func Parse(r io.Reader, logger *slog.Logger) ([]Session, error) {
	return nil, nil
}
```

- [ ] **Step 2: Write test for assistant message with usage (AC1)**

```go
package parser

import (
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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/parser/ -run TestParse_AssistantWithUsage -v`
Expected: FAIL — `want 1 session, got 0`

- [ ] **Step 4: Write test for tool_use and tool_result (AC2)**

```go
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
```

- [ ] **Step 5: Write test for session aggregation (AC3)**

```go
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
```

- [ ] **Step 6: Write test for malformed line (AC4)**

```go
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
```

- [ ] **Step 7: Write test for empty file (AC5)**

```go
func TestParse_EmptyFile(t *testing.T) {
	sessions, err := Parse(strings.NewReader(""), testLogger)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("want 0 sessions for empty file, got %d", len(sessions))
	}
}
```

- [ ] **Step 8: Write test for skipping non-message types**

```go
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
```

- [ ] **Step 9: Write test for user message with string content**

```go
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
```

- [ ] **Step 10: Run all tests to verify they fail**

Run: `go test ./internal/parser/ -v`
Expected: all 7 tests FAIL

- [ ] **Step 11: Commit test file**

```bash
git add internal/parser/parser_test.go internal/parser/parser.go
git commit -m "test(parser): add failing tests for JSONL parser (AC1-AC5)"
```

---

### Task 3: Implement the parser

**Files:**
- Modify: `internal/parser/parser.go`

- [ ] **Step 1: Implement Parse function**

```go
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
		logger.Info("file is empty or has no parseable messages")
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
```

- [ ] **Step 2: Add missing import in types.go**

Add `"encoding/json"` to the import in `types.go` (needed for `json.RawMessage` fields):

```go
package parser

import (
	"encoding/json"
	"time"
)
```

- [ ] **Step 3: Run all tests to verify they pass**

Run: `go test ./internal/parser/ -v`
Expected: all 7 tests PASS

- [ ] **Step 4: Commit**

```bash
git add internal/parser/
git commit -m "feat(parser): implement JSONL parse engine with session aggregation"
```

---

### Task 4: Wire parser into pipeline (`cmd/exporter/main.go`)

**Files:**
- Modify: `cmd/exporter/main.go`

- [ ] **Step 1: Update runPipeline to open and parse each file**

Replace the `// 5. [Story 2+ placeholder: process files]` section and the mark-processed loop:

```go
// In the import block, add:
// "os" (already present)
// "github.com/tim80411/claude-code-otel-exporter/internal/parser"

// Replace from "log.Info("files to process"..." through "store.Save()":

	log.Info("files to process", "count", len(files))

	// 5. Parse each file
	var allSessions []parser.Session
	for _, f := range files {
		file, err := os.Open(f.Path)
		if err != nil {
			log.Warn("skipping file", "path", f.Path, "error", err)
			continue
		}
		sessions, err := parser.Parse(file, log)
		file.Close()
		if err != nil {
			log.Warn("parse failed", "path", f.Path, "error", err)
			continue
		}
		for i := range sessions {
			sessions[i].ProjectName = f.ProjectName
		}
		allSessions = append(allSessions, sessions...)
	}

	log.Info("parsed sessions", "count", len(allSessions))

	// 6. [Story 3+ placeholder: export sessions via OTLP]

	// 7. Mark all files as processed
	now := time.Now().UTC()
	for _, f := range files {
		store.MarkProcessed(f.Path, state.FileState{
			ModTime:     f.ModTime,
			Size:        f.Size,
			ProcessedAt: now,
		})
	}

	// 8. Save state
	if err := store.Save(); err != nil {
		return PipelineResult{}, err
	}

	return PipelineResult{FilesProcessed: len(files)}, nil
```

- [ ] **Step 2: Add ProjectName field to Session**

In `internal/parser/types.go`, add `ProjectName` to `Session`:

```go
type Session struct {
	SessionID   string
	ProjectName string // set by caller after parsing
	Messages    []Message
	StartTime   time.Time
	EndTime     time.Time
}
```

- [ ] **Step 3: Build and verify**

Run: `go build ./cmd/exporter/`
Expected: success

- [ ] **Step 4: Run all tests**

Run: `go test ./... -v`
Expected: all tests PASS (state, reader, parser)

- [ ] **Step 5: Integration test — run against real data**

```bash
make run
```

Expected: logs show `files to process` count and `parsed sessions` count > 0. Second run should show 0 or near-0 new files.

- [ ] **Step 6: Commit**

```bash
git add cmd/exporter/main.go internal/parser/types.go
git commit -m "feat: wire JSONL parser into pipeline"
```

---

## Self-Review Checklist

| Spec AC | Task | Status |
|---------|------|--------|
| AC1: assistant message with usage | Task 2 Step 2 + Task 3 | Covered — test parses all 4 token types |
| AC2: tool_use and tool_result | Task 2 Step 4 + Task 3 | Covered — test checks name, tool_use_id, is_error |
| AC3: session aggregation by sessionId | Task 2 Step 5 + Task 3 | Covered — 20 messages aggregated, start/end time checked |
| AC4: malformed line skipped | Task 2 Step 6 + Task 3 | Covered — bad JSON skipped, other lines parsed |
| AC5: empty file | Task 2 Step 7 + Task 3 | Covered — returns empty sessions |
| Out of scope: no parentUuid chain reconstruction | — | Correct: not implemented |
| Out of scope: no semantic analysis | — | Correct: not implemented |
| Out of scope: no stats-cache.json | — | Correct: not implemented |

**Type consistency check:** `Session`, `Message`, `Usage`, `ContentBlock` — names match across types.go, parser.go, parser_test.go, and main.go. `ProjectName` added to Session in Task 4 Step 2, used in Task 4 Step 1.

**Placeholder scan:** Task 4 Step 1 has `// 6. [Story 3+ placeholder: export sessions via OTLP]` — this is intentional, matching Story 3 scope.
