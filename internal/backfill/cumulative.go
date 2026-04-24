package backfill

import (
	"sort"
	"time"

	"github.com/tim80411/claude-code-otel-exporter/internal/metrics"
	"github.com/tim80411/claude-code-otel-exporter/internal/parser"
)

// SessionTotals is the running total of a single session's contribution to metrics.
// Stored per session in state so that re-processing the same session (as new
// messages are appended to its JSONL file) only adds the delta to cumulative.
type SessionTotals struct {
	Sessions            int64              `json:"sessions"`
	InputTokens         int64              `json:"input_tokens"`
	OutputTokens        int64              `json:"output_tokens"`
	CacheReadTokens     int64              `json:"cache_read_tokens"`
	CacheCreationTokens int64              `json:"cache_creation_tokens"`
	CostByModel         map[string]float64 `json:"cost_by_model,omitempty"`
	RequestsByModel     map[string]int64   `json:"requests_by_model,omitempty"`
	LinesAdded          int64              `json:"lines_added"`
	LinesRemoved        int64              `json:"lines_removed"`
	Commits             int64              `json:"commits"`
	PullRequests        int64              `json:"pull_requests"`
}

// CumulativeTotals holds the running, monotonically non-decreasing totals
// for all metrics across all runs. Persisted in state so that counter
// semantics survive CronJob restarts.
type CumulativeTotals struct {
	Sessions            float64            `json:"sessions"`
	InputTokens         float64            `json:"input_tokens"`
	OutputTokens        float64            `json:"output_tokens"`
	CacheReadTokens     float64            `json:"cache_read_tokens"`
	CacheCreationTokens float64            `json:"cache_creation_tokens"`
	CostByModel         map[string]float64 `json:"cost_by_model,omitempty"`
	RequestsByModel     map[string]float64 `json:"requests_by_model,omitempty"`
	LinesAdded          float64            `json:"lines_added"`
	LinesRemoved        float64            `json:"lines_removed"`
	Commits             float64            `json:"commits"`
	PullRequests        float64            `json:"pull_requests"`
}

// ComputeSessionTotals aggregates a session's full metrics into a SessionTotals.
// Calling this twice on the same session with the same messages yields the same result.
func ComputeSessionTotals(sess parser.Session) SessionTotals {
	totals := SessionTotals{
		Sessions:        1,
		CostByModel:     map[string]float64{},
		RequestsByModel: map[string]int64{},
	}

	type modelTokens struct{ input, output, cacheRead, cacheCreation, requests int }
	perModel := make(map[string]*modelTokens)
	for _, msg := range sess.Messages {
		if msg.Role != "assistant" || msg.Usage == nil {
			continue
		}
		mt, ok := perModel[msg.Model]
		if !ok {
			mt = &modelTokens{}
			perModel[msg.Model] = mt
		}
		mt.input += msg.Usage.InputTokens
		mt.output += msg.Usage.OutputTokens
		mt.cacheRead += msg.Usage.CacheReadInputTokens
		mt.cacheCreation += msg.Usage.CacheCreationInputTokens
		mt.requests++
	}

	for model, mt := range perModel {
		totals.InputTokens += int64(mt.input)
		totals.OutputTokens += int64(mt.output)
		totals.CacheReadTokens += int64(mt.cacheRead)
		totals.CacheCreationTokens += int64(mt.cacheCreation)
		totals.RequestsByModel[model] += int64(mt.requests)

		pricing, known := metrics.LookupPricing(model)
		if !known {
			continue
		}
		cost := metrics.ComputeCost(mt.input, mt.output, mt.cacheRead, mt.cacheCreation, pricing)
		if cost > 0 {
			totals.CostByModel[model] += cost
		}
	}

	stats := metrics.ExtractOutputStats(sess)
	totals.LinesAdded = int64(stats.LinesAdded)
	totals.LinesRemoved = int64(stats.LinesRemoved)
	totals.Commits = int64(stats.Commits)
	totals.PullRequests = int64(stats.PullRequests)

	return totals
}

// ApplySessionDelta adds (current - previous) to cumulative.
// Negative deltas (which shouldn't happen for well-behaved counters but could
// arise from pricing table changes or buggy re-parsing) are clamped to 0 so
// that cumulative stays monotonically non-decreasing.
func ApplySessionDelta(cumulative *CumulativeTotals, previous, current SessionTotals) {
	if cumulative.CostByModel == nil {
		cumulative.CostByModel = map[string]float64{}
	}
	if cumulative.RequestsByModel == nil {
		cumulative.RequestsByModel = map[string]float64{}
	}

	cumulative.Sessions += clampNonNeg(float64(current.Sessions - previous.Sessions))
	cumulative.InputTokens += clampNonNeg(float64(current.InputTokens - previous.InputTokens))
	cumulative.OutputTokens += clampNonNeg(float64(current.OutputTokens - previous.OutputTokens))
	cumulative.CacheReadTokens += clampNonNeg(float64(current.CacheReadTokens - previous.CacheReadTokens))
	cumulative.CacheCreationTokens += clampNonNeg(float64(current.CacheCreationTokens - previous.CacheCreationTokens))
	cumulative.LinesAdded += clampNonNeg(float64(current.LinesAdded - previous.LinesAdded))
	cumulative.LinesRemoved += clampNonNeg(float64(current.LinesRemoved - previous.LinesRemoved))
	cumulative.Commits += clampNonNeg(float64(current.Commits - previous.Commits))
	cumulative.PullRequests += clampNonNeg(float64(current.PullRequests - previous.PullRequests))

	for model, v := range current.CostByModel {
		cumulative.CostByModel[model] += clampNonNeg(v - previous.CostByModel[model])
	}
	for model, v := range current.RequestsByModel {
		cumulative.RequestsByModel[model] += clampNonNeg(float64(v - previous.RequestsByModel[model]))
	}
}

func clampNonNeg(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}

// BuildSnapshotSeries emits one sample per metric series at ts, reading values
// from cumulative. Series with zero values are skipped so Prometheus doesn't
// have to store noise.
func BuildSnapshotSeries(cumulative CumulativeTotals, ts time.Time, jobLabel string) []TimeSeries {
	tsMs := ts.UnixMilli()
	var series []TimeSeries

	emit := func(name string, value float64, extra ...Label) {
		if value <= 0 {
			return
		}
		labels := []Label{
			{Name: "__name__", Value: name},
			{Name: "job", Value: jobLabel},
		}
		labels = append(labels, extra...)
		sort.Slice(labels, func(i, j int) bool { return labels[i].Name < labels[j].Name })
		series = append(series, TimeSeries{
			Labels:  labels,
			Samples: []Sample{{TimestampMs: tsMs, Value: value}},
		})
	}

	emit("claude_code_session_count_total", cumulative.Sessions)
	emit("claude_code_token_usage_total", cumulative.InputTokens, Label{Name: "type", Value: "input"})
	emit("claude_code_token_usage_total", cumulative.OutputTokens, Label{Name: "type", Value: "output"})
	emit("claude_code_token_usage_total", cumulative.CacheReadTokens, Label{Name: "type", Value: "cacheRead"})
	emit("claude_code_token_usage_total", cumulative.CacheCreationTokens, Label{Name: "type", Value: "cacheCreation"})
	emit("claude_code_lines_of_code_count_total", cumulative.LinesAdded, Label{Name: "type", Value: "added"})
	emit("claude_code_lines_of_code_count_total", cumulative.LinesRemoved, Label{Name: "type", Value: "removed"})
	emit("claude_code_commit_count_total", cumulative.Commits)
	emit("claude_code_pull_request_count_total", cumulative.PullRequests)

	// Sort model keys for deterministic output.
	models := make([]string, 0, len(cumulative.CostByModel)+len(cumulative.RequestsByModel))
	seen := map[string]bool{}
	for m := range cumulative.CostByModel {
		if !seen[m] {
			seen[m] = true
			models = append(models, m)
		}
	}
	for m := range cumulative.RequestsByModel {
		if !seen[m] {
			seen[m] = true
			models = append(models, m)
		}
	}
	sort.Strings(models)

	for _, m := range models {
		emit("claude_code_cost_usage_total", cumulative.CostByModel[m], Label{Name: "model", Value: m})
		emit("claude_code_api_request_count_total", cumulative.RequestsByModel[m], Label{Name: "model", Value: m})
	}

	return series
}
