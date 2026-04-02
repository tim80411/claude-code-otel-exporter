package parser

import (
	"encoding/json"
	"time"
)

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
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // string (user text) or []ContentBlock
	Usage   *Usage          `json:"usage,omitempty"`
	Model   string          `json:"model,omitempty"`
}

// Usage holds token counts from assistant messages.
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	Ephemeral5mInputTokens   int `json:"ephemeral_5m_input_tokens"`
	Ephemeral1hInputTokens   int `json:"ephemeral_1h_input_tokens"`
}

// ContentBlock is one element of the content array.
type ContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Thinking string `json:"thinking,omitempty"`
	// tool_use fields
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
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
	Model     string
	Usage     *Usage
	Content   []ContentBlock
}

// Session aggregates all messages belonging to the same sessionId.
type Session struct {
	SessionID   string
	ProjectName string // set by caller after parsing
	Messages    []Message
	StartTime   time.Time
	EndTime     time.Time
}
