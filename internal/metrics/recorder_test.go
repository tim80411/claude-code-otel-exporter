package metrics

import (
	"context"
	"log/slog"
	"math"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/tim80411/claude-code-otel-exporter/internal/parser"
)

// --- helpers ---

var testLogger = slog.Default()

func setupRecorder(t *testing.T) (*Recorder, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := mp.Meter("test")
	rec, err := NewRecorder(meter, testLogger)
	if err != nil {
		t.Fatal(err)
	}
	return rec, reader
}

func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	return rm
}

func findMetric(rm metricdata.ResourceMetrics, name string) *metricdata.Metrics {
	for _, sm := range rm.ScopeMetrics {
		for i, m := range sm.Metrics {
			if m.Name == name {
				return &sm.Metrics[i]
			}
		}
	}
	return nil
}

func intSumValue(m *metricdata.Metrics, attrs ...attribute.KeyValue) int64 {
	if m == nil {
		return 0
	}
	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		return 0
	}
	set := attribute.NewSet(attrs...)
	for _, dp := range sum.DataPoints {
		if dp.Attributes.Equals(&set) {
			return dp.Value
		}
	}
	return 0
}

func floatSumValue(m *metricdata.Metrics, attrs ...attribute.KeyValue) float64 {
	if m == nil {
		return 0
	}
	sum, ok := m.Data.(metricdata.Sum[float64])
	if !ok {
		return 0
	}
	set := attribute.NewSet(attrs...)
	for _, dp := range sum.DataPoints {
		if dp.Attributes.Equals(&set) {
			return dp.Value
		}
	}
	return 0
}

func intTotalSum(m *metricdata.Metrics) int64 {
	if m == nil {
		return 0
	}
	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		return 0
	}
	var total int64
	for _, dp := range sum.DataPoints {
		total += dp.Value
	}
	return total
}

func floatTotalSum(m *metricdata.Metrics) float64 {
	if m == nil {
		return 0
	}
	sum, ok := m.Data.(metricdata.Sum[float64])
	if !ok {
		return 0
	}
	var total float64
	for _, dp := range sum.DataPoints {
		total += dp.Value
	}
	return total
}

// --- time helpers ---

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func ts(offset time.Duration) time.Time { return t0.Add(offset) }

func makeMsg(role string, t time.Time, model string, usage *parser.Usage) parser.Message {
	return parser.Message{Role: role, Timestamp: t, Model: model, Usage: usage}
}

func makeUsage(input, output int) *parser.Usage {
	return &parser.Usage{InputTokens: input, OutputTokens: output}
}

func makeFullUsage(input, output, cacheRead, cacheCreation int) *parser.Usage {
	return &parser.Usage{
		InputTokens:              input,
		OutputTokens:             output,
		CacheReadInputTokens:     cacheRead,
		CacheCreationInputTokens: cacheCreation,
	}
}

func almostEqual(a, b, epsilon float64) bool {
	return math.Abs(a-b) < epsilon
}

// ==================== DeduplicateSessions tests ====================

func TestDedup_Empty(t *testing.T) {
	result := DeduplicateSessions(nil)
	if result != nil {
		t.Fatalf("expected nil, got %v", result)
	}
}

func TestDedup_SingleSession(t *testing.T) {
	sessions := []parser.Session{
		{SessionID: "s1", Messages: []parser.Message{makeMsg("user", ts(0), "", nil)}},
	}
	result := DeduplicateSessions(sessions)
	if len(result) != 1 {
		t.Fatalf("expected 1 session, got %d", len(result))
	}
	if result[0].SessionID != "s1" {
		t.Fatalf("expected session s1, got %s", result[0].SessionID)
	}
	if len(result[0].Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result[0].Messages))
	}
}

func TestDedup_MergesSameID(t *testing.T) {
	sessions := []parser.Session{
		{
			SessionID: "s1",
			Messages:  []parser.Message{makeMsg("user", ts(0), "", nil)},
		},
		{
			SessionID: "s1",
			Messages:  []parser.Message{makeMsg("assistant", ts(time.Minute), "claude-opus-4-6", makeUsage(100, 50))},
		},
	}
	result := DeduplicateSessions(sessions)
	if len(result) != 1 {
		t.Fatalf("expected 1 session, got %d", len(result))
	}
	if len(result[0].Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result[0].Messages))
	}
	if result[0].StartTime != ts(0) {
		t.Errorf("expected StartTime %v, got %v", ts(0), result[0].StartTime)
	}
	if result[0].EndTime != ts(time.Minute) {
		t.Errorf("expected EndTime %v, got %v", ts(time.Minute), result[0].EndTime)
	}
}

func TestDedup_DifferentIDs(t *testing.T) {
	sessions := []parser.Session{
		{SessionID: "s1", Messages: []parser.Message{makeMsg("user", ts(0), "", nil)}},
		{SessionID: "s2", Messages: []parser.Message{makeMsg("user", ts(time.Minute), "", nil)}},
	}
	result := DeduplicateSessions(sessions)
	if len(result) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(result))
	}
}

// ==================== Record tests ====================

func TestRecord_SingleSessionWithTokens(t *testing.T) {
	rec, reader := setupRecorder(t)
	sessions := []parser.Session{
		{
			SessionID: "s1",
			Messages: []parser.Message{
				makeMsg("user", ts(0), "", nil),
				makeMsg("assistant", ts(time.Minute), "claude-opus-4-6", makeUsage(100, 50)),
			},
		},
	}

	rec.Record(context.Background(), sessions)
	rm := collectMetrics(t, reader)

	sc := findMetric(rm, "claude_code.session.count")
	if v := intTotalSum(sc); v != 1 {
		t.Errorf("session.count = %d, want 1", v)
	}

	tu := findMetric(rm, "claude_code.token.usage")
	if v := intSumValue(tu, attribute.String("type", "input")); v != 100 {
		t.Errorf("token.usage input = %d, want 100", v)
	}
	if v := intSumValue(tu, attribute.String("type", "output")); v != 50 {
		t.Errorf("token.usage output = %d, want 50", v)
	}
}

func TestRecord_UserOnlySession(t *testing.T) {
	rec, reader := setupRecorder(t)
	sessions := []parser.Session{
		{
			SessionID: "s1",
			Messages:  []parser.Message{makeMsg("user", ts(0), "", nil)},
		},
	}

	rec.Record(context.Background(), sessions)
	rm := collectMetrics(t, reader)

	sc := findMetric(rm, "claude_code.session.count")
	if v := intTotalSum(sc); v != 1 {
		t.Errorf("session.count = %d, want 1", v)
	}

	tu := findMetric(rm, "claude_code.token.usage")
	if v := intTotalSum(tu); v != 0 {
		t.Errorf("token.usage = %d, want 0", v)
	}
}

func TestRecord_MultipleSessions(t *testing.T) {
	rec, reader := setupRecorder(t)
	sessions := []parser.Session{
		{
			SessionID: "s1",
			Messages: []parser.Message{
				makeMsg("assistant", ts(0), "claude-opus-4-6", makeUsage(100, 50)),
			},
		},
		{
			SessionID: "s2",
			Messages: []parser.Message{
				makeMsg("assistant", ts(time.Minute), "claude-opus-4-6", makeUsage(200, 75)),
			},
		},
	}

	rec.Record(context.Background(), sessions)
	rm := collectMetrics(t, reader)

	sc := findMetric(rm, "claude_code.session.count")
	if v := intTotalSum(sc); v != 2 {
		t.Errorf("session.count = %d, want 2", v)
	}

	tu := findMetric(rm, "claude_code.token.usage")
	if v := intSumValue(tu, attribute.String("type", "input")); v != 300 {
		t.Errorf("token.usage input = %d, want 300", v)
	}
	if v := intSumValue(tu, attribute.String("type", "output")); v != 125 {
		t.Errorf("token.usage output = %d, want 125", v)
	}
}

func TestRecord_EmptySessions(t *testing.T) {
	rec, reader := setupRecorder(t)
	rec.Record(context.Background(), nil)
	rm := collectMetrics(t, reader)

	sc := findMetric(rm, "claude_code.session.count")
	if v := intTotalSum(sc); v != 0 {
		t.Errorf("session.count = %d, want 0", v)
	}
}

func TestRecord_CrossFileDedupIntegration(t *testing.T) {
	raw := []parser.Session{
		{
			SessionID: "s1",
			Messages:  []parser.Message{makeMsg("user", ts(0), "", nil)},
		},
		{
			SessionID: "s1",
			Messages: []parser.Message{
				makeMsg("assistant", ts(time.Minute), "claude-opus-4-6", makeUsage(500, 200)),
			},
		},
		{
			SessionID: "s2",
			Messages: []parser.Message{
				makeMsg("assistant", ts(2*time.Minute), "claude-opus-4-6", makeUsage(50, 25)),
			},
		},
	}

	deduped := DeduplicateSessions(raw)
	if len(deduped) != 2 {
		t.Fatalf("expected 2 sessions after dedup, got %d", len(deduped))
	}

	rec, reader := setupRecorder(t)
	rec.Record(context.Background(), deduped)
	rm := collectMetrics(t, reader)

	sc := findMetric(rm, "claude_code.session.count")
	if v := intTotalSum(sc); v != 2 {
		t.Errorf("session.count = %d, want 2", v)
	}

	tu := findMetric(rm, "claude_code.token.usage")
	if v := intSumValue(tu, attribute.String("type", "input")); v != 550 {
		t.Errorf("token.usage input = %d, want 550", v)
	}
	if v := intSumValue(tu, attribute.String("type", "output")); v != 225 {
		t.Errorf("token.usage output = %d, want 225", v)
	}
}

// ==================== TIM-126: Cost + Advanced Token tests ====================

func TestRecord_CostCalculation(t *testing.T) {
	// claude-opus-4-6: input $15/MTok, output $75/MTok
	// 10000 input + 2000 output = (10000*15 + 2000*75) / 1_000_000 = 0.15 + 0.15 = 0.30
	rec, reader := setupRecorder(t)
	sessions := []parser.Session{
		{
			SessionID: "s1",
			Messages: []parser.Message{
				makeMsg("assistant", ts(0), "claude-opus-4-6", makeUsage(10000, 2000)),
			},
		},
	}

	rec.Record(context.Background(), sessions)
	rm := collectMetrics(t, reader)

	cu := findMetric(rm, "claude_code.cost.usage")
	cost := floatSumValue(cu, attribute.String("model", "claude-opus-4-6"))
	if !almostEqual(cost, 0.30, 0.0001) {
		t.Errorf("cost = %f, want 0.30", cost)
	}
}

func TestRecord_DifferentModels(t *testing.T) {
	rec, reader := setupRecorder(t)
	sessions := []parser.Session{
		{
			SessionID: "s1",
			Messages: []parser.Message{
				makeMsg("assistant", ts(0), "claude-opus-4-6", makeUsage(10000, 2000)),
				makeMsg("assistant", ts(time.Minute), "claude-sonnet-4-6", makeUsage(10000, 2000)),
			},
		},
	}

	rec.Record(context.Background(), sessions)
	rm := collectMetrics(t, reader)

	cu := findMetric(rm, "claude_code.cost.usage")
	// opus: (10000*15 + 2000*75) / 1M = 0.30
	opusCost := floatSumValue(cu, attribute.String("model", "claude-opus-4-6"))
	if !almostEqual(opusCost, 0.30, 0.0001) {
		t.Errorf("opus cost = %f, want 0.30", opusCost)
	}
	// sonnet: (10000*3 + 2000*15) / 1M = 0.06
	sonnetCost := floatSumValue(cu, attribute.String("model", "claude-sonnet-4-6"))
	if !almostEqual(sonnetCost, 0.06, 0.0001) {
		t.Errorf("sonnet cost = %f, want 0.06", sonnetCost)
	}
}

func TestRecord_CacheTokenTypes(t *testing.T) {
	rec, reader := setupRecorder(t)
	sessions := []parser.Session{
		{
			SessionID: "s1",
			Messages: []parser.Message{
				makeMsg("assistant", ts(0), "claude-opus-4-6", makeFullUsage(1000, 500, 5000, 3000)),
			},
		},
	}

	rec.Record(context.Background(), sessions)
	rm := collectMetrics(t, reader)

	tu := findMetric(rm, "claude_code.token.usage")
	if v := intSumValue(tu, attribute.String("type", "input")); v != 1000 {
		t.Errorf("input = %d, want 1000", v)
	}
	if v := intSumValue(tu, attribute.String("type", "output")); v != 500 {
		t.Errorf("output = %d, want 500", v)
	}
	if v := intSumValue(tu, attribute.String("type", "cacheRead")); v != 5000 {
		t.Errorf("cacheRead = %d, want 5000", v)
	}
	if v := intSumValue(tu, attribute.String("type", "cacheCreation")); v != 3000 {
		t.Errorf("cacheCreation = %d, want 3000", v)
	}

	// Cost includes cache tokens: (1000*15 + 500*75 + 5000*1.5 + 3000*18.75) / 1M
	// = (15000 + 37500 + 7500 + 56250) / 1M = 116250 / 1M = 0.11625
	cu := findMetric(rm, "claude_code.cost.usage")
	cost := floatSumValue(cu, attribute.String("model", "claude-opus-4-6"))
	if !almostEqual(cost, 0.11625, 0.0001) {
		t.Errorf("cost = %f, want 0.11625", cost)
	}
}

func TestRecord_UnknownModel(t *testing.T) {
	rec, reader := setupRecorder(t)
	sessions := []parser.Session{
		{
			SessionID: "s1",
			Messages: []parser.Message{
				makeMsg("assistant", ts(0), "unknown-model-x", makeUsage(1000, 500)),
			},
		},
	}

	rec.Record(context.Background(), sessions)
	rm := collectMetrics(t, reader)

	// Tokens still recorded
	tu := findMetric(rm, "claude_code.token.usage")
	if v := intSumValue(tu, attribute.String("type", "input")); v != 1000 {
		t.Errorf("input = %d, want 1000", v)
	}
	if v := intSumValue(tu, attribute.String("type", "output")); v != 500 {
		t.Errorf("output = %d, want 500", v)
	}

	// Cost NOT recorded for unknown model
	cu := findMetric(rm, "claude_code.cost.usage")
	if v := floatTotalSum(cu); v != 0 {
		t.Errorf("cost = %f, want 0 (unknown model)", v)
	}
}
