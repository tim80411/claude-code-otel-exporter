package backfill

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/klauspost/compress/s2"
)

const maxSamplesPerBatch = 500

// Writer sends Prometheus Remote Write requests.
type Writer struct {
	endpoint string
	auth     string
	client   *http.Client
	logger   *slog.Logger
}

// NewWriter creates a Remote Write client.
// endpoint is the Prometheus base URL (e.g. "https://prometheus.example.com");
// the path /api/v1/write is appended automatically.
func NewWriter(endpoint, auth string, logger *slog.Logger) *Writer {
	return &Writer{
		endpoint: endpoint + "/api/v1/write",
		auth:     auth,
		client:   &http.Client{Timeout: 30 * time.Second},
		logger:   logger,
	}
}

// Write sends all time series to the Remote Write endpoint.
// Series are batched so each request contains at most maxSamplesPerBatch samples.
func (w *Writer) Write(ctx context.Context, series []TimeSeries) error {
	if len(series) == 0 {
		return nil
	}

	var batch []TimeSeries
	var batchSamples int

	for _, ts := range series {
		n := len(ts.Samples)
		if batchSamples+n > maxSamplesPerBatch && len(batch) > 0 {
			if err := w.writeBatch(ctx, batch); err != nil {
				return err
			}
			batch = nil
			batchSamples = 0
		}
		batch = append(batch, ts)
		batchSamples += n
	}

	if len(batch) > 0 {
		return w.writeBatch(ctx, batch)
	}
	return nil
}

func (w *Writer) writeBatch(ctx context.Context, series []TimeSeries) error {
	raw := encodeWriteRequest(series)
	compressed := s2.EncodeSnappy(nil, raw)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.endpoint, bytes.NewReader(compressed))
	if err != nil {
		return fmt.Errorf("backfill: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Content-Encoding", "snappy")
	req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
	if w.auth != "" {
		req.Header.Set("Authorization", "Basic "+w.auth)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("backfill: remote write: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("backfill: remote write returned status %d: %s", resp.StatusCode, string(body))
}
