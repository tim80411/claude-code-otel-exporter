package backfill

import (
	"sort"
	"time"

	"github.com/tim80411/claude-code-otel-exporter/internal/metrics"
	"github.com/tim80411/claude-code-otel-exporter/internal/parser"
)

// Bucket holds aggregated metrics for one time interval.
type Bucket struct {
	Time         time.Time
	Sessions     int64
	TokenInput   int64
	TokenOutput  int64
	TokenCacheR  int64
	TokenCacheC  int64
	CostByModel  map[string]float64
	LinesAdded   int64
	LinesRemoved int64
	Commits      int64
	PullRequests int64
}

// Aggregate groups sessions into hourly buckets by StartTime.
// Sessions must be pre-deduplicated.
func Aggregate(sessions []parser.Session, bucketSize time.Duration) []Bucket {
	if len(sessions) == 0 {
		return nil
	}

	index := make(map[time.Time]*Bucket)

	for _, sess := range sessions {
		key := sess.StartTime.Truncate(bucketSize)
		b, ok := index[key]
		if !ok {
			b = &Bucket{Time: key, CostByModel: make(map[string]float64)}
			index[key] = b
		}

		b.Sessions++

		// Token accumulation per model (replicates recorder.go logic).
		type modelTokens struct {
			input, output, cacheRead, cacheCreation int
		}
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
		}

		for model, mt := range perModel {
			b.TokenInput += int64(mt.input)
			b.TokenOutput += int64(mt.output)
			b.TokenCacheR += int64(mt.cacheRead)
			b.TokenCacheC += int64(mt.cacheCreation)

			pricing, known := metrics.LookupPricing(model)
			if !known {
				continue
			}
			cost := metrics.ComputeCost(mt.input, mt.output, mt.cacheRead, mt.cacheCreation, pricing)
			if cost > 0 {
				b.CostByModel[model] += cost
			}
		}

		// Output stats.
		stats := metrics.ExtractOutputStats(sess)
		b.LinesAdded += int64(stats.LinesAdded)
		b.LinesRemoved += int64(stats.LinesRemoved)
		b.Commits += int64(stats.Commits)
		b.PullRequests += int64(stats.PullRequests)
	}

	result := make([]Bucket, 0, len(index))
	for _, b := range index {
		result = append(result, *b)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Time.Before(result[j].Time)
	})
	return result
}

// TimeSeries is a named metric with labels and samples for Remote Write.
type TimeSeries struct {
	Labels  []Label
	Samples []Sample
}

// Label is a key-value pair for a TimeSeries.
type Label struct {
	Name  string
	Value string
}

// Sample is a (timestamp_ms, value) pair.
type Sample struct {
	TimestampMs int64
	Value       float64
}

// BuildTimeSeries converts sorted buckets into cumulative TimeSeries.
// Values are prefix-summed so Prometheus sees monotonically increasing counters.
func BuildTimeSeries(buckets []Bucket, jobLabel string) []TimeSeries {
	if len(buckets) == 0 {
		return nil
	}

	// Collect all models across all buckets for stable iteration.
	modelSet := make(map[string]struct{})
	for _, b := range buckets {
		for m := range b.CostByModel {
			modelSet[m] = struct{}{}
		}
	}
	models := make([]string, 0, len(modelSet))
	for m := range modelSet {
		models = append(models, m)
	}
	sort.Strings(models)

	var series []TimeSeries

	// Helper to build labels with __name__ first, sorted.
	makeLabels := func(name string, extra ...Label) []Label {
		labels := []Label{{Name: "__name__", Value: name}, {Name: "job", Value: jobLabel}}
		labels = append(labels, extra...)
		sort.Slice(labels, func(i, j int) bool { return labels[i].Name < labels[j].Name })
		return labels
	}

	// session_count_total
	if s := buildCumulativeSeries(
		makeLabels("claude_code_session_count_total"),
		buckets, func(b Bucket) float64 { return float64(b.Sessions) },
	); len(s.Samples) > 0 {
		series = append(series, s)
	}

	// token_usage_total by type
	for _, tokenType := range []struct {
		label string
		fn    func(Bucket) float64
	}{
		{"input", func(b Bucket) float64 { return float64(b.TokenInput) }},
		{"output", func(b Bucket) float64 { return float64(b.TokenOutput) }},
		{"cacheRead", func(b Bucket) float64 { return float64(b.TokenCacheR) }},
		{"cacheCreation", func(b Bucket) float64 { return float64(b.TokenCacheC) }},
	} {
		s := buildCumulativeSeries(
			makeLabels("claude_code_token_usage_total", Label{Name: "type", Value: tokenType.label}),
			buckets, tokenType.fn,
		)
		if len(s.Samples) > 0 {
			series = append(series, s)
		}
	}

	// cost_usage_total by model
	for _, model := range models {
		m := model
		s := buildCumulativeSeries(
			makeLabels("claude_code_cost_usage_total", Label{Name: "model", Value: m}),
			buckets, func(b Bucket) float64 { return b.CostByModel[m] },
		)
		if len(s.Samples) > 0 {
			series = append(series, s)
		}
	}

	// lines_of_code_count_total by type
	for _, lineType := range []struct {
		label string
		fn    func(Bucket) float64
	}{
		{"added", func(b Bucket) float64 { return float64(b.LinesAdded) }},
		{"removed", func(b Bucket) float64 { return float64(b.LinesRemoved) }},
	} {
		s := buildCumulativeSeries(
			makeLabels("claude_code_lines_of_code_count_total", Label{Name: "type", Value: lineType.label}),
			buckets, lineType.fn,
		)
		if len(s.Samples) > 0 {
			series = append(series, s)
		}
	}

	// commit_count_total
	s := buildCumulativeSeries(
		makeLabels("claude_code_commit_count_total"),
		buckets, func(b Bucket) float64 { return float64(b.Commits) },
	)
	if len(s.Samples) > 0 {
		series = append(series, s)
	}

	// pull_request_count_total
	s = buildCumulativeSeries(
		makeLabels("claude_code_pull_request_count_total"),
		buckets, func(b Bucket) float64 { return float64(b.PullRequests) },
	)
	if len(s.Samples) > 0 {
		series = append(series, s)
	}

	return series
}

// buildCumulativeSeries creates a single TimeSeries with prefix-summed samples.
// Only includes samples where the cumulative value is non-zero.
func buildCumulativeSeries(labels []Label, buckets []Bucket, valueFunc func(Bucket) float64) TimeSeries {
	ts := TimeSeries{Labels: labels}
	var cumulative float64
	for _, b := range buckets {
		cumulative += valueFunc(b)
		if cumulative > 0 {
			ts.Samples = append(ts.Samples, Sample{
				TimestampMs: b.Time.UnixMilli(),
				Value:       cumulative,
			})
		}
	}
	return ts
}
