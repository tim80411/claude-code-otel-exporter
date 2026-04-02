package events

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// LokiClient pushes log entries to Loki via HTTP API.
type LokiClient struct {
	endpoint  string
	basicAuth string
	client    *http.Client
}

// NewLokiClient creates a client for the Loki push API.
func NewLokiClient(endpoint, basicAuth string) *LokiClient {
	return &LokiClient{
		endpoint:  endpoint,
		basicAuth: basicAuth,
		client:    &http.Client{Timeout: 30 * time.Second},
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

// Push sends events to Loki as a single batch.
func (c *LokiClient) Push(ctx context.Context, events []Event) error {
	if len(events) == 0 {
		return nil
	}

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

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("loki: push returned status %d", resp.StatusCode)
	}

	return nil
}
