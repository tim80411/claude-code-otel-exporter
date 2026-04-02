package metrics

import (
	"context"
	"encoding/json"
	"testing"

	"go.opentelemetry.io/otel/attribute"

	"github.com/tim80411/claude-code-otel-exporter/internal/parser"
)

func makeToolUse(id, name string, input interface{}) parser.ContentBlock {
	raw, _ := json.Marshal(input)
	return parser.ContentBlock{Type: "tool_use", ID: id, Name: name, Input: raw}
}

func makeToolResult(toolUseID string, isError bool) parser.ContentBlock {
	return parser.ContentBlock{Type: "tool_result", ToolUseID: toolUseID, IsError: isError}
}

// ==================== ExtractOutputStats tests ====================

func TestOutput_WriteLines(t *testing.T) {
	sess := parser.Session{
		SessionID: "s1",
		Messages: []parser.Message{
			{
				Role: "assistant",
				Content: []parser.ContentBlock{
					makeToolUse("t1", "Write", map[string]string{
						"file_path": "/tmp/file.go",
						"content":   "line1\nline2\nline3",
					}),
				},
			},
			{
				Role:    "user",
				Content: []parser.ContentBlock{makeToolResult("t1", false)},
			},
		},
	}

	stats := ExtractOutputStats(sess)
	if stats.LinesAdded != 3 {
		t.Errorf("LinesAdded = %d, want 3", stats.LinesAdded)
	}
}

func TestOutput_EditLines(t *testing.T) {
	sess := parser.Session{
		SessionID: "s1",
		Messages: []parser.Message{
			{
				Role: "assistant",
				Content: []parser.ContentBlock{
					makeToolUse("t1", "Edit", map[string]string{
						"file_path":  "/tmp/file.go",
						"old_string": "old line",
						"new_string": "new line 1\nnew line 2\nnew line 3",
					}),
				},
			},
			{
				Role:    "user",
				Content: []parser.ContentBlock{makeToolResult("t1", false)},
			},
		},
	}

	stats := ExtractOutputStats(sess)
	if stats.LinesAdded != 3 {
		t.Errorf("LinesAdded = %d, want 3", stats.LinesAdded)
	}
	if stats.LinesRemoved != 1 {
		t.Errorf("LinesRemoved = %d, want 1", stats.LinesRemoved)
	}
}

func TestOutput_GitCommit(t *testing.T) {
	sess := parser.Session{
		SessionID: "s1",
		Messages: []parser.Message{
			{
				Role: "assistant",
				Content: []parser.ContentBlock{
					makeToolUse("t1", "Bash", map[string]string{
						"command": `git commit -m "fix: login bug"`,
					}),
				},
			},
			{
				Role:    "user",
				Content: []parser.ContentBlock{makeToolResult("t1", false)},
			},
		},
	}

	stats := ExtractOutputStats(sess)
	if stats.Commits != 1 {
		t.Errorf("Commits = %d, want 1", stats.Commits)
	}
}

func TestOutput_GHPRCreate(t *testing.T) {
	sess := parser.Session{
		SessionID: "s1",
		Messages: []parser.Message{
			{
				Role: "assistant",
				Content: []parser.ContentBlock{
					makeToolUse("t1", "Bash", map[string]string{
						"command": `gh pr create --title "feat: add auth"`,
					}),
				},
			},
			{
				Role:    "user",
				Content: []parser.ContentBlock{makeToolResult("t1", false)},
			},
		},
	}

	stats := ExtractOutputStats(sess)
	if stats.PullRequests != 1 {
		t.Errorf("PullRequests = %d, want 1", stats.PullRequests)
	}
}

func TestOutput_FailedCommitNotCounted(t *testing.T) {
	sess := parser.Session{
		SessionID: "s1",
		Messages: []parser.Message{
			{
				Role: "assistant",
				Content: []parser.ContentBlock{
					makeToolUse("t1", "Bash", map[string]string{
						"command": "git commit -m 'test'",
					}),
				},
			},
			{
				Role:    "user",
				Content: []parser.ContentBlock{makeToolResult("t1", true)}, // error!
			},
		},
	}

	stats := ExtractOutputStats(sess)
	if stats.Commits != 0 {
		t.Errorf("Commits = %d, want 0 (failed commit)", stats.Commits)
	}
}

func TestOutput_NoToolUse(t *testing.T) {
	sess := parser.Session{
		SessionID: "s1",
		Messages: []parser.Message{
			{Role: "user", Content: []parser.ContentBlock{{Type: "text", Text: "hello"}}},
			{Role: "assistant", Content: []parser.ContentBlock{{Type: "text", Text: "hi"}}},
		},
	}

	stats := ExtractOutputStats(sess)
	if stats.LinesAdded != 0 || stats.LinesRemoved != 0 || stats.Commits != 0 || stats.PullRequests != 0 {
		t.Errorf("expected all zeros, got %+v", stats)
	}
}

// ==================== Recorder integration with output metrics ====================

func TestRecord_OutputMetrics(t *testing.T) {
	rec, reader := setupRecorder(t)
	sessions := []parser.Session{
		{
			SessionID: "s1",
			Messages: []parser.Message{
				{
					Role: "assistant",
					Content: []parser.ContentBlock{
						makeToolUse("t1", "Write", map[string]string{
							"file_path": "/tmp/file.go",
							"content":   "a\nb\nc\nd\ne",
						}),
						makeToolUse("t2", "Bash", map[string]string{
							"command": `git commit -m "feat: add file"`,
						}),
						makeToolUse("t3", "Bash", map[string]string{
							"command": `gh pr create --title "feat: new feature"`,
						}),
					},
				},
				{
					Role: "user",
					Content: []parser.ContentBlock{
						makeToolResult("t1", false),
						makeToolResult("t2", false),
						makeToolResult("t3", false),
					},
				},
			},
		},
	}

	rec.Record(context.Background(), sessions)
	rm := collectMetrics(t, reader)

	loc := findMetric(rm, "claude_code.lines_of_code.count")
	if v := intSumValue(loc, attribute.String("type", "added")); v != 5 {
		t.Errorf("lines_of_code added = %d, want 5", v)
	}

	cc := findMetric(rm, "claude_code.commit.count")
	if v := intTotalSum(cc); v != 1 {
		t.Errorf("commit.count = %d, want 1", v)
	}

	prc := findMetric(rm, "claude_code.pull_request.count")
	if v := intTotalSum(prc); v != 1 {
		t.Errorf("pull_request.count = %d, want 1", v)
	}
}
