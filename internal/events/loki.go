package events

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// LokiClient pushes log entries to Loki via HTTP API.
type LokiClient struct {
	endpoint  string
	basicAuth string
	client    *http.Client
	logger    *slog.Logger
}

// NewLokiClient creates a client for the Loki push API.
func NewLokiClient(endpoint, basicAuth string, logger *slog.Logger) *LokiClient {
	return &LokiClient{
		endpoint:  endpoint,
		basicAuth: basicAuth,
		client:    &http.Client{Timeout: 30 * time.Second},
		logger:    logger,
	}
}

// lokiPushRequest is the Loki push API payload.
type lokiPushRequest struct {
	Streams []lokiStream `json:"streams"`
}

type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}

const defaultBatchSize = 1000

// Push sends events to Loki in batches to avoid 413 Request Entity Too Large.
func (c *LokiClient) Push(ctx context.Context, events []Event) error {
	if len(events) == 0 {
		return nil
	}

	for i := 0; i < len(events); i += defaultBatchSize {
		end := i + defaultBatchSize
		if end > len(events) {
			end = len(events)
		}
		if err := c.pushBatch(ctx, events[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func (c *LokiClient) pushBatch(ctx context.Context, events []Event) error {
	values := make([][]string, 0, len(events))
	for _, ev := range events {
		bodyJSON, err := json.Marshal(ev.Body)
		if err != nil {
			continue
		}
		ts := strconv.FormatInt(ev.Timestamp.UnixNano(), 10)
		values = append(values, []string{ts, string(bodyJSON)})
	}

	payload := lokiPushRequest{
		Streams: []lokiStream{
			{
				Stream: map[string]string{"service_name": "claude-code"},
				Values: values,
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("loki: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("loki: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.basicAuth != "" {
		req.Header.Set("Authorization", "Basic "+c.basicAuth)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("loki: push: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		c.logger.Warn("loki: batch rejected (likely too old), skipping", "status", resp.StatusCode, "body", string(body))
		return nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("loki: push returned status %d", resp.StatusCode)
	}

	return nil
}
