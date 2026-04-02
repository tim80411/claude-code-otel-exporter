package metrics

import (
	"encoding/json"
	"strings"

	"github.com/tim80411/claude-code-otel-exporter/internal/parser"
)

// OutputStats holds extracted output metrics from a session.
type OutputStats struct {
	LinesAdded   int
	LinesRemoved int
	Commits      int
	PullRequests int
}

// ExtractOutputStats analyzes tool_use and tool_result content blocks
// in a session to extract output metrics.
func ExtractOutputStats(sess parser.Session) OutputStats {
	var stats OutputStats

	// Build a map of tool_use ID → tool_use block for linking.
	type toolUse struct {
		Name  string
		Input json.RawMessage
	}
	toolUses := make(map[string]toolUse)

	// Build a map of tool_use ID → tool_result for error checking.
	type toolResult struct {
		IsError bool
	}
	toolResults := make(map[string]toolResult)

	// First pass: collect all tool_uses and tool_results.
	for _, msg := range sess.Messages {
		for _, block := range msg.Content {
			switch block.Type {
			case "tool_use":
				toolUses[block.ID] = toolUse{Name: block.Name, Input: block.Input}
			case "tool_result":
				toolResults[block.ToolUseID] = toolResult{IsError: block.IsError}
			}
		}
	}

	// Second pass: analyze each tool_use with its result.
	for id, tu := range toolUses {
		result, hasResult := toolResults[id]
		isError := hasResult && result.IsError

		switch tu.Name {
		case "Write":
			if !isError {
				added := countWriteLines(tu.Input)
				stats.LinesAdded += added
			}
		case "Edit":
			if !isError {
				added, removed := countEditLines(tu.Input)
				stats.LinesAdded += added
				stats.LinesRemoved += removed
			}
		case "Bash":
			if !isError {
				cmd := extractBashCommand(tu.Input)
				if isGitCommit(cmd) {
					stats.Commits++
				}
				if isGHPRCreate(cmd) {
					stats.PullRequests++
				}
			}
		}
	}

	return stats
}

// countWriteLines counts lines in a Write tool_use's content field.
func countWriteLines(input json.RawMessage) int {
	var payload struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &payload); err != nil || payload.Content == "" {
		return 0
	}
	return strings.Count(payload.Content, "\n") + 1
}

// countEditLines returns (added, removed) line counts from an Edit tool_use.
func countEditLines(input json.RawMessage) (added, removed int) {
	var payload struct {
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return 0, 0
	}
	oldLines := countLines(payload.OldString)
	newLines := countLines(payload.NewString)
	return newLines, oldLines
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// extractBashCommand extracts the command string from a Bash tool_use input.
func extractBashCommand(input json.RawMessage) string {
	var payload struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return ""
	}
	return payload.Command
}

func isGitCommit(cmd string) bool {
	return strings.Contains(cmd, "git commit")
}

func isGHPRCreate(cmd string) bool {
	return strings.Contains(cmd, "gh pr create")
}
