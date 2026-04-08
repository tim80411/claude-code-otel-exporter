package backfill

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klauspost/compress/s2"
)

func TestWriter_Headers(t *testing.T) {
	var gotReq *http.Request
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r.Clone(r.Context())
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	writer := NewWriter(srv.URL, "dXNlcjpwYXNz", slog.Default())

	series := []TimeSeries{{
		Labels:  []Label{{Name: "__name__", Value: "test_total"}, {Name: "job", Value: "test"}},
		Samples: []Sample{{TimestampMs: 1000, Value: 1.0}},
	}}

	err := writer.Write(t.Context(), series)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if gotReq.Header.Get("Content-Type") != "application/x-protobuf" {
		t.Errorf("Content-Type: got %q", gotReq.Header.Get("Content-Type"))
	}
	if gotReq.Header.Get("Content-Encoding") != "snappy" {
		t.Errorf("Content-Encoding: got %q", gotReq.Header.Get("Content-Encoding"))
	}
	if gotReq.Header.Get("X-Prometheus-Remote-Write-Version") != "0.1.0" {
		t.Errorf("X-Prometheus-Remote-Write-Version: got %q", gotReq.Header.Get("X-Prometheus-Remote-Write-Version"))
	}
	if gotReq.Header.Get("Authorization") != "Basic dXNlcjpwYXNz" {
		t.Errorf("Authorization: got %q", gotReq.Header.Get("Authorization"))
	}

	// Verify body is valid snappy.
	_, err = s2.Decode(nil, gotBody)
	if err != nil {
		t.Fatalf("snappy decode: %v", err)
	}
}

func TestWriter_NoAuth(t *testing.T) {
	var gotReq *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r.Clone(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	writer := NewWriter(srv.URL, "", slog.Default())

	err := writer.Write(t.Context(), []TimeSeries{{
		Labels:  []Label{{Name: "__name__", Value: "test"}},
		Samples: []Sample{{TimestampMs: 1000, Value: 1.0}},
	}})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if gotReq.Header.Get("Authorization") != "" {
		t.Errorf("expected no Authorization header, got %q", gotReq.Header.Get("Authorization"))
	}
}

func TestWriter_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	writer := NewWriter(srv.URL, "", slog.Default())

	err := writer.Write(t.Context(), []TimeSeries{{
		Labels:  []Label{{Name: "__name__", Value: "test"}},
		Samples: []Sample{{TimestampMs: 1000, Value: 1.0}},
	}})
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
}

func TestWriter_Batching(t *testing.T) {
	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	writer := NewWriter(srv.URL, "", slog.Default())

	// Create series that exceeds maxSamplesPerBatch.
	var series []TimeSeries
	for i := 0; i < 3; i++ {
		samples := make([]Sample, 200)
		for j := range samples {
			samples[j] = Sample{TimestampMs: int64(i*200 + j), Value: float64(j)}
		}
		series = append(series, TimeSeries{
			Labels:  []Label{{Name: "__name__", Value: "test"}},
			Samples: samples,
		})
	}

	err := writer.Write(t.Context(), series)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	// 600 samples total, batch size 500 → at least 2 requests.
	if requestCount < 2 {
		t.Errorf("expected at least 2 batches, got %d", requestCount)
	}
}

func TestWriter_Empty(t *testing.T) {
	writer := NewWriter("http://unused", "", slog.Default())
	err := writer.Write(t.Context(), nil)
	if err != nil {
		t.Fatalf("Write(nil): %v", err)
	}
}

func TestEncodeWriteRequest_Roundtrip(t *testing.T) {
	series := []TimeSeries{
		{
			Labels: []Label{
				{Name: "__name__", Value: "test_total"},
				{Name: "job", Value: "exporter"},
			},
			Samples: []Sample{
				{TimestampMs: 1709280000000, Value: 42.0},
				{TimestampMs: 1709283600000, Value: 99.5},
			},
		},
	}

	data := encodeWriteRequest(series)
	if len(data) == 0 {
		t.Fatal("encoded data is empty")
	}

	// Verify the first byte is the correct protobuf tag:
	// field 1, wire type 2 (length-delimited) = (1 << 3) | 2 = 0x0A
	if data[0] != 0x0A {
		t.Errorf("first byte: got 0x%02X, want 0x0A", data[0])
	}
}
