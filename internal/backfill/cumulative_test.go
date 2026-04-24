package backfill

import (
	"testing"
	"time"

	"github.com/tim80411/claude-code-otel-exporter/internal/parser"
)

func TestComputeSessionTotals_SingleModel(t *testing.T) {
	sess := parser.Session{
		SessionID: "s1",
		StartTime: mustParseTime(t, "2025-03-01T10:00:00Z"),
		Messages: []parser.Message{
			{Role: "assistant", Model: "claude-sonnet-4-6", Timestamp: mustParseTime(t, "2025-03-01T10:00:00Z"),
				Usage: &parser.Usage{InputTokens: 100, OutputTokens: 50, CacheReadInputTokens: 10, CacheCreationInputTokens: 5}},
			{Role: "assistant", Model: "claude-sonnet-4-6", Timestamp: mustParseTime(t, "2025-03-01T10:01:00Z"),
				Usage: &parser.Usage{InputTokens: 200, OutputTokens: 100}},
		},
	}

	totals := ComputeSessionTotals(sess)

	if totals.Sessions != 1 {
		t.Errorf("Sessions: got %d, want 1", totals.Sessions)
	}
	if totals.InputTokens != 300 {
		t.Errorf("InputTokens: got %d, want 300", totals.InputTokens)
	}
	if totals.OutputTokens != 150 {
		t.Errorf("OutputTokens: got %d, want 150", totals.OutputTokens)
	}
	if totals.CacheReadTokens != 10 {
		t.Errorf("CacheReadTokens: got %d, want 10", totals.CacheReadTokens)
	}
	if totals.CacheCreationTokens != 5 {
		t.Errorf("CacheCreationTokens: got %d, want 5", totals.CacheCreationTokens)
	}
	if totals.RequestsByModel["claude-sonnet-4-6"] != 2 {
		t.Errorf("Requests: got %d, want 2", totals.RequestsByModel["claude-sonnet-4-6"])
	}
	if totals.CostByModel["claude-sonnet-4-6"] <= 0 {
		t.Errorf("Cost: got %f, want > 0", totals.CostByModel["claude-sonnet-4-6"])
	}
}

func TestComputeSessionTotals_Deterministic(t *testing.T) {
	// Same input must produce same totals so that re-processing is idempotent.
	sess := parser.Session{
		SessionID: "s1",
		StartTime: mustParseTime(t, "2025-03-01T10:00:00Z"),
		Messages: []parser.Message{
			{Role: "assistant", Model: "claude-sonnet-4-6", Timestamp: mustParseTime(t, "2025-03-01T10:00:00Z"),
				Usage: &parser.Usage{InputTokens: 100, OutputTokens: 50}},
		},
	}

	a := ComputeSessionTotals(sess)
	b := ComputeSessionTotals(sess)

	if a.InputTokens != b.InputTokens || a.OutputTokens != b.OutputTokens {
		t.Errorf("ComputeSessionTotals is not deterministic: %+v vs %+v", a, b)
	}
	if a.CostByModel["claude-sonnet-4-6"] != b.CostByModel["claude-sonnet-4-6"] {
		t.Errorf("Cost mismatch: %f vs %f", a.CostByModel["claude-sonnet-4-6"], b.CostByModel["claude-sonnet-4-6"])
	}
}

func TestApplySessionDelta_InitialSession(t *testing.T) {
	cumulative := CumulativeTotals{}
	current := SessionTotals{
		Sessions:     1,
		InputTokens:  100,
		OutputTokens: 50,
		CostByModel:  map[string]float64{"sonnet": 0.5},
	}

	ApplySessionDelta(&cumulative, SessionTotals{}, current)

	if cumulative.Sessions != 1 {
		t.Errorf("Sessions: got %f, want 1", cumulative.Sessions)
	}
	if cumulative.InputTokens != 100 {
		t.Errorf("InputTokens: got %f, want 100", cumulative.InputTokens)
	}
	if cumulative.CostByModel["sonnet"] != 0.5 {
		t.Errorf("Cost: got %f, want 0.5", cumulative.CostByModel["sonnet"])
	}
}

func TestApplySessionDelta_IncrementalUpdate(t *testing.T) {
	// Simulate: session S seen twice with growing totals.
	cumulative := CumulativeTotals{}

	run1 := SessionTotals{
		Sessions: 1, InputTokens: 100, OutputTokens: 50,
		CostByModel:     map[string]float64{"sonnet": 0.5},
		RequestsByModel: map[string]int64{"sonnet": 3},
	}
	ApplySessionDelta(&cumulative, SessionTotals{}, run1)

	// Second run: session grew.
	run2 := SessionTotals{
		Sessions: 1, InputTokens: 250, OutputTokens: 120,
		CostByModel:     map[string]float64{"sonnet": 1.2},
		RequestsByModel: map[string]int64{"sonnet": 7},
	}
	ApplySessionDelta(&cumulative, run1, run2)

	// Cumulative reflects the actual total, not the sum of sessions counted twice.
	if cumulative.Sessions != 1 {
		t.Errorf("Sessions should stay 1 (same session): got %f", cumulative.Sessions)
	}
	if cumulative.InputTokens != 250 {
		t.Errorf("InputTokens: got %f, want 250", cumulative.InputTokens)
	}
	if cumulative.OutputTokens != 120 {
		t.Errorf("OutputTokens: got %f, want 120", cumulative.OutputTokens)
	}
	if cumulative.CostByModel["sonnet"] != 1.2 {
		t.Errorf("Cost: got %f, want 1.2", cumulative.CostByModel["sonnet"])
	}
	if cumulative.RequestsByModel["sonnet"] != 7 {
		t.Errorf("Requests: got %f, want 7", cumulative.RequestsByModel["sonnet"])
	}
}

func TestApplySessionDelta_ClampsNegative(t *testing.T) {
	// If pricing or parsing logic regresses, a session's "new" totals might be
	// smaller than what we last stored. Cumulative must not go backwards.
	cumulative := CumulativeTotals{Sessions: 5, InputTokens: 500}

	prev := SessionTotals{Sessions: 1, InputTokens: 300}
	current := SessionTotals{Sessions: 1, InputTokens: 200} // shrunk!

	ApplySessionDelta(&cumulative, prev, current)

	if cumulative.Sessions != 5 {
		t.Errorf("Sessions shouldn't change (delta 0): got %f", cumulative.Sessions)
	}
	if cumulative.InputTokens != 500 {
		t.Errorf("InputTokens should stay 500 (negative delta clamped): got %f", cumulative.InputTokens)
	}
}

func TestBuildSnapshotSeries_SkipsZero(t *testing.T) {
	cumulative := CumulativeTotals{
		Sessions:    5,
		InputTokens: 100,
		// All other fields are zero.
	}

	series := BuildSnapshotSeries(cumulative, time.Unix(1700000000, 0), "test-job")

	// Expect: session_count + token_usage{type=input}. Nothing else.
	if len(series) != 2 {
		t.Errorf("expected 2 series with non-zero values, got %d", len(series))
		for _, s := range series {
			for _, l := range s.Labels {
				if l.Name == "__name__" {
					t.Logf("  series: %s", l.Value)
				}
			}
		}
	}
}

func TestBuildSnapshotSeries_OneSamplePerSeries(t *testing.T) {
	cumulative := CumulativeTotals{
		Sessions:     3,
		InputTokens:  100,
		OutputTokens: 50,
		Commits:      2,
		CostByModel:  map[string]float64{"sonnet": 0.5, "opus": 1.0},
	}
	ts := time.Unix(1700000000, 0)

	series := BuildSnapshotSeries(cumulative, ts, "test")

	tsMs := ts.UnixMilli()
	for _, s := range series {
		if len(s.Samples) != 1 {
			t.Errorf("series has %d samples, expected exactly 1", len(s.Samples))
		}
		if s.Samples[0].TimestampMs != tsMs {
			t.Errorf("sample timestamp: got %d, want %d", s.Samples[0].TimestampMs, tsMs)
		}
	}
}

func TestBuildSnapshotSeries_JobLabelOnAll(t *testing.T) {
	cumulative := CumulativeTotals{
		Sessions:    1,
		InputTokens: 1,
		CostByModel: map[string]float64{"sonnet": 0.1},
	}
	series := BuildSnapshotSeries(cumulative, time.Unix(1, 0), "my-job")

	for _, s := range series {
		foundJob := false
		for _, l := range s.Labels {
			if l.Name == "job" && l.Value == "my-job" {
				foundJob = true
			}
		}
		if !foundJob {
			name := ""
			for _, l := range s.Labels {
				if l.Name == "__name__" {
					name = l.Value
				}
			}
			t.Errorf("series %q missing job label", name)
		}
	}
}

func TestEndToEnd_CrossRunMonotonic(t *testing.T) {
	// Simulate two runs with overlapping + new sessions; cumulative must be
	// monotonic (strictly >= previous) across runs.
	snapshots := map[string]SessionTotals{}
	cumulative := CumulativeTotals{}

	// Run 1: 2 new sessions.
	sess1 := parser.Session{
		SessionID: "s1", StartTime: mustParseTime(t, "2025-03-01T10:00:00Z"),
		Messages: []parser.Message{{Role: "assistant", Model: "claude-sonnet-4-6",
			Timestamp: mustParseTime(t, "2025-03-01T10:00:00Z"),
			Usage:     &parser.Usage{InputTokens: 100, OutputTokens: 50}}},
	}
	sess2 := parser.Session{
		SessionID: "s2", StartTime: mustParseTime(t, "2025-03-01T11:00:00Z"),
		Messages: []parser.Message{{Role: "assistant", Model: "claude-sonnet-4-6",
			Timestamp: mustParseTime(t, "2025-03-01T11:00:00Z"),
			Usage:     &parser.Usage{InputTokens: 200, OutputTokens: 100}}},
	}

	for _, sess := range []parser.Session{sess1, sess2} {
		cur := ComputeSessionTotals(sess)
		ApplySessionDelta(&cumulative, snapshots[sess.SessionID], cur)
		snapshots[sess.SessionID] = cur
	}

	afterRun1 := cumulative
	if afterRun1.Sessions != 2 {
		t.Errorf("after run 1: Sessions=%f, want 2", afterRun1.Sessions)
	}
	if afterRun1.InputTokens != 300 {
		t.Errorf("after run 1: InputTokens=%f, want 300", afterRun1.InputTokens)
	}

	// Run 2: session s1 continued (more messages), session s3 is new.
	sess1Extended := parser.Session{
		SessionID: "s1", StartTime: mustParseTime(t, "2025-03-01T10:00:00Z"),
		Messages: []parser.Message{
			{Role: "assistant", Model: "claude-sonnet-4-6",
				Timestamp: mustParseTime(t, "2025-03-01T10:00:00Z"),
				Usage:     &parser.Usage{InputTokens: 100, OutputTokens: 50}},
			{Role: "assistant", Model: "claude-sonnet-4-6",
				Timestamp: mustParseTime(t, "2025-03-01T10:30:00Z"),
				Usage:     &parser.Usage{InputTokens: 80, OutputTokens: 40}},
		},
	}
	sess3 := parser.Session{
		SessionID: "s3", StartTime: mustParseTime(t, "2025-03-01T12:00:00Z"),
		Messages: []parser.Message{{Role: "assistant", Model: "claude-sonnet-4-6",
			Timestamp: mustParseTime(t, "2025-03-01T12:00:00Z"),
			Usage:     &parser.Usage{InputTokens: 50, OutputTokens: 25}}},
	}

	for _, sess := range []parser.Session{sess1Extended, sess3} {
		cur := ComputeSessionTotals(sess)
		ApplySessionDelta(&cumulative, snapshots[sess.SessionID], cur)
		snapshots[sess.SessionID] = cur
	}

	// Monotonic checks.
	if cumulative.Sessions < afterRun1.Sessions {
		t.Errorf("Sessions regressed: %f < %f", cumulative.Sessions, afterRun1.Sessions)
	}
	if cumulative.InputTokens < afterRun1.InputTokens {
		t.Errorf("InputTokens regressed: %f < %f", cumulative.InputTokens, afterRun1.InputTokens)
	}

	// Correct final values: 3 sessions (s1, s2, s3), tokens = 100+80+200+50 = 430.
	if cumulative.Sessions != 3 {
		t.Errorf("after run 2: Sessions=%f, want 3", cumulative.Sessions)
	}
	if cumulative.InputTokens != 430 {
		t.Errorf("after run 2: InputTokens=%f, want 430", cumulative.InputTokens)
	}
}
