package backfill

import (
	"testing"
	"time"

	"github.com/tim80411/claude-code-otel-exporter/internal/parser"
)

func TestAggregate_Empty(t *testing.T) {
	result := Aggregate(nil, time.Hour)
	if result != nil {
		t.Fatalf("expected nil, got %d buckets", len(result))
	}
}

func TestAggregate_SingleBucket(t *testing.T) {
	sessions := []parser.Session{
		makeSession(t, "2025-03-01T10:15:00Z", "claude-sonnet-4-6", 100, 50, 0, 0),
		makeSession(t, "2025-03-01T10:45:00Z", "claude-sonnet-4-6", 200, 100, 0, 0),
	}

	buckets := Aggregate(sessions, time.Hour)
	if len(buckets) != 1 {
		t.Fatalf("expected 1 bucket, got %d", len(buckets))
	}

	b := buckets[0]
	if b.Sessions != 2 {
		t.Errorf("sessions: got %d, want 2", b.Sessions)
	}
	if b.TokenInput != 300 {
		t.Errorf("token input: got %d, want 300", b.TokenInput)
	}
	if b.TokenOutput != 150 {
		t.Errorf("token output: got %d, want 150", b.TokenOutput)
	}
}

func TestAggregate_MultipleBuckets(t *testing.T) {
	sessions := []parser.Session{
		makeSession(t, "2025-03-01T10:00:00Z", "claude-sonnet-4-6", 100, 50, 0, 0),
		makeSession(t, "2025-03-01T11:30:00Z", "claude-sonnet-4-6", 200, 100, 0, 0),
		makeSession(t, "2025-03-01T12:00:00Z", "claude-sonnet-4-6", 300, 150, 0, 0),
	}

	buckets := Aggregate(sessions, time.Hour)
	if len(buckets) != 3 {
		t.Fatalf("expected 3 buckets, got %d", len(buckets))
	}

	// Verify chronological order.
	for i := 1; i < len(buckets); i++ {
		if !buckets[i].Time.After(buckets[i-1].Time) {
			t.Errorf("buckets not sorted: %v >= %v", buckets[i-1].Time, buckets[i].Time)
		}
	}
}

func TestAggregate_MultiModel(t *testing.T) {
	sess := parser.Session{
		SessionID: "s1",
		StartTime: mustParseTime(t, "2025-03-01T10:00:00Z"),
		EndTime:   mustParseTime(t, "2025-03-01T10:05:00Z"),
		Messages: []parser.Message{
			{Role: "assistant", Model: "claude-sonnet-4-6", Timestamp: mustParseTime(t, "2025-03-01T10:00:00Z"),
				Usage: &parser.Usage{InputTokens: 100, OutputTokens: 50}},
			{Role: "assistant", Model: "claude-opus-4-6", Timestamp: mustParseTime(t, "2025-03-01T10:05:00Z"),
				Usage: &parser.Usage{InputTokens: 200, OutputTokens: 100}},
		},
	}

	buckets := Aggregate([]parser.Session{sess}, time.Hour)
	if len(buckets) != 1 {
		t.Fatalf("expected 1 bucket, got %d", len(buckets))
	}

	b := buckets[0]
	if len(b.CostByModel) != 2 {
		t.Errorf("expected 2 models in cost, got %d", len(b.CostByModel))
	}
	if b.CostByModel["claude-sonnet-4-6"] <= 0 {
		t.Errorf("expected sonnet cost > 0, got %f", b.CostByModel["claude-sonnet-4-6"])
	}
	if b.CostByModel["claude-opus-4-6"] <= 0 {
		t.Errorf("expected opus cost > 0, got %f", b.CostByModel["claude-opus-4-6"])
	}
}

func TestBuildTimeSeries_PrefixSum(t *testing.T) {
	buckets := []Bucket{
		{Time: mustParseTime(t, "2025-03-01T10:00:00Z"), Sessions: 3},
		{Time: mustParseTime(t, "2025-03-01T11:00:00Z"), Sessions: 5},
		{Time: mustParseTime(t, "2025-03-01T12:00:00Z"), Sessions: 2},
	}

	series := BuildTimeSeries(buckets, "test-job", time.Time{})

	// Find session count series.
	var sessionSeries *TimeSeries
	for i, s := range series {
		for _, l := range s.Labels {
			if l.Name == "__name__" && l.Value == "claude_code_session_count_total" {
				sessionSeries = &series[i]
				break
			}
		}
	}

	if sessionSeries == nil {
		t.Fatal("session count series not found")
	}

	if len(sessionSeries.Samples) != 3 {
		t.Fatalf("expected 3 samples, got %d", len(sessionSeries.Samples))
	}

	// Prefix-sum: 3, 8, 10
	expected := []float64{3, 8, 10}
	for i, want := range expected {
		if sessionSeries.Samples[i].Value != want {
			t.Errorf("sample[%d]: got %f, want %f", i, sessionSeries.Samples[i].Value, want)
		}
	}
}

func TestBuildTimeSeries_Labels(t *testing.T) {
	buckets := []Bucket{
		{
			Time: mustParseTime(t, "2025-03-01T10:00:00Z"), Sessions: 1,
			TokenInput: 100, TokenOutput: 50,
			CostByModel: map[string]float64{"claude-sonnet-4-6": 0.5},
		},
	}

	series := BuildTimeSeries(buckets, "my-job", time.Time{})

	// Verify job label is present on all series.
	for _, s := range series {
		hasJob := false
		for _, l := range s.Labels {
			if l.Name == "job" && l.Value == "my-job" {
				hasJob = true
			}
		}
		if !hasJob {
			name := ""
			for _, l := range s.Labels {
				if l.Name == "__name__" {
					name = l.Value
				}
			}
			t.Errorf("series %q missing job label", name)
		}
	}

	// Verify labels are sorted.
	for _, s := range series {
		for i := 1; i < len(s.Labels); i++ {
			if s.Labels[i].Name < s.Labels[i-1].Name {
				t.Errorf("labels not sorted: %q before %q", s.Labels[i-1].Name, s.Labels[i].Name)
			}
		}
	}
}

func TestBuildTimeSeries_SkipsZero(t *testing.T) {
	buckets := []Bucket{
		{Time: mustParseTime(t, "2025-03-01T10:00:00Z"), Sessions: 1},
	}

	series := BuildTimeSeries(buckets, "test", time.Time{})

	// No commit or PR series should exist (all zero).
	for _, s := range series {
		for _, l := range s.Labels {
			if l.Name == "__name__" && (l.Value == "claude_code_commit_count_total" || l.Value == "claude_code_pull_request_count_total") {
				t.Errorf("unexpected zero-value series: %s", l.Value)
			}
		}
	}
}

// Helpers

func makeSession(t *testing.T, ts string, model string, input, output, cacheRead, cacheCreation int) parser.Session {
	t.Helper()
	start := mustParseTime(t, ts)
	return parser.Session{
		SessionID: "sess-" + ts,
		StartTime: start,
		EndTime:   start.Add(5 * time.Minute),
		Messages: []parser.Message{
			{
				Role:      "assistant",
				Model:     model,
				Timestamp: start,
				Usage: &parser.Usage{
					InputTokens:              input,
					OutputTokens:             output,
					CacheReadInputTokens:     cacheRead,
					CacheCreationInputTokens: cacheCreation,
				},
			},
		},
	}
}

func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return ts
}
