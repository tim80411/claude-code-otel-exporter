package metrics

import (
	"context"
	"log/slog"
	"sort"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/tim80411/claude-code-otel-exporter/internal/parser"
)

// Recorder records session, token, cost, output, and health metrics via OTEL counters.
type Recorder struct {
	sessionCount     metric.Int64Counter
	tokenUsage       metric.Int64Counter
	costUsage        metric.Float64Counter
	linesOfCode      metric.Int64Counter
	commitCount      metric.Int64Counter
	pullRequestCount metric.Int64Counter
	filesProcessed   metric.Int64Counter
	parseErrors      metric.Int64Counter
	processingTime   metric.Int64Counter
	logger           *slog.Logger
}

// NewRecorder creates a Recorder with instruments from the given Meter.
func NewRecorder(m metric.Meter, logger *slog.Logger) (*Recorder, error) {
	sc, err := m.Int64Counter("claude_code.session.count",
		metric.WithDescription("Number of unique Claude Code sessions"),
	)
	if err != nil {
		return nil, err
	}

	tu, err := m.Int64Counter("claude_code.token.usage",
		metric.WithDescription("Token usage across Claude Code sessions"),
		metric.WithUnit("tokens"),
	)
	if err != nil {
		return nil, err
	}

	cu, err := m.Float64Counter("claude_code.cost.usage",
		metric.WithDescription("Cost in USD across Claude Code sessions"),
		metric.WithUnit("USD"),
	)
	if err != nil {
		return nil, err
	}

	loc, err := m.Int64Counter("claude_code.lines_of_code.count",
		metric.WithDescription("Lines of code added/removed by Claude Code"),
	)
	if err != nil {
		return nil, err
	}

	cc, err := m.Int64Counter("claude_code.commit.count",
		metric.WithDescription("Git commits made by Claude Code"),
	)
	if err != nil {
		return nil, err
	}

	prc, err := m.Int64Counter("claude_code.pull_request.count",
		metric.WithDescription("Pull requests created by Claude Code"),
	)
	if err != nil {
		return nil, err
	}

	fp, err := m.Int64Counter("claude_code.exporter.files_processed",
		metric.WithDescription("Files processed by the exporter"),
	)
	if err != nil {
		return nil, err
	}

	pe, err := m.Int64Counter("claude_code.exporter.parse_errors",
		metric.WithDescription("Parse errors encountered by the exporter"),
	)
	if err != nil {
		return nil, err
	}

	pt, err := m.Int64Counter("claude_code.exporter.processing_time",
		metric.WithDescription("Processing time in milliseconds"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, err
	}

	return &Recorder{
		sessionCount:     sc,
		tokenUsage:       tu,
		costUsage:        cu,
		linesOfCode:      loc,
		commitCount:      cc,
		pullRequestCount: prc,
		filesProcessed:   fp,
		parseErrors:      pe,
		processingTime:   pt,
		logger:           logger,
	}, nil
}

// Record records metrics for the given sessions.
// Sessions should be deduplicated before calling Record.
func (r *Recorder) Record(ctx context.Context, sessions []parser.Session) {
	for _, sess := range sessions {
		r.sessionCount.Add(ctx, 1)
		r.recordTokensAndCost(ctx, sess)
		r.recordOutput(ctx, sess)
	}
}

// RecordHealth records exporter health metrics.
func (r *Recorder) RecordHealth(ctx context.Context, filesProcessed, parseErrors int, processingTimeMs int64) {
	r.filesProcessed.Add(ctx, int64(filesProcessed))
	if parseErrors > 0 {
		r.parseErrors.Add(ctx, int64(parseErrors))
	}
	r.processingTime.Add(ctx, processingTimeMs)
}

func (r *Recorder) recordOutput(ctx context.Context, sess parser.Session) {
	stats := ExtractOutputStats(sess)

	typeAttr := func(t string) metric.AddOption {
		return metric.WithAttributes(attribute.String("type", t))
	}

	if stats.LinesAdded > 0 {
		r.linesOfCode.Add(ctx, int64(stats.LinesAdded), typeAttr("added"))
	}
	if stats.LinesRemoved > 0 {
		r.linesOfCode.Add(ctx, int64(stats.LinesRemoved), typeAttr("removed"))
	}
	if stats.Commits > 0 {
		r.commitCount.Add(ctx, int64(stats.Commits))
	}
	if stats.PullRequests > 0 {
		r.pullRequestCount.Add(ctx, int64(stats.PullRequests))
	}
}

func (r *Recorder) recordTokensAndCost(ctx context.Context, sess parser.Session) {
	// Accumulate tokens per model for cost calculation.
	type modelTokens struct {
		input, output, cacheRead, cacheCreation int
	}
	perModel := make(map[string]*modelTokens)

	for _, msg := range sess.Messages {
		if msg.Role != "assistant" || msg.Usage == nil {
			continue
		}

		model := msg.Model
		mt, ok := perModel[model]
		if !ok {
			mt = &modelTokens{}
			perModel[model] = mt
		}
		mt.input += msg.Usage.InputTokens
		mt.output += msg.Usage.OutputTokens
		mt.cacheRead += msg.Usage.CacheReadInputTokens
		mt.cacheCreation += msg.Usage.CacheCreationInputTokens
	}

	typeAttr := func(t string) metric.AddOption {
		return metric.WithAttributes(attribute.String("type", t))
	}

	for model, mt := range perModel {
		if mt.input > 0 {
			r.tokenUsage.Add(ctx, int64(mt.input), typeAttr("input"))
		}
		if mt.output > 0 {
			r.tokenUsage.Add(ctx, int64(mt.output), typeAttr("output"))
		}
		if mt.cacheRead > 0 {
			r.tokenUsage.Add(ctx, int64(mt.cacheRead), typeAttr("cacheRead"))
		}
		if mt.cacheCreation > 0 {
			r.tokenUsage.Add(ctx, int64(mt.cacheCreation), typeAttr("cacheCreation"))
		}

		// Cost calculation
		pricing, known := LookupPricing(model)
		if !known {
			r.logger.Warn("unknown model, skipping cost", "model", model)
			continue
		}
		cost := ComputeCost(mt.input, mt.output, mt.cacheRead, mt.cacheCreation, pricing)
		if cost > 0 {
			r.costUsage.Add(ctx, cost, metric.WithAttributes(
				attribute.String("model", model),
			))
		}
	}
}

// DeduplicateSessions merges sessions with the same SessionID.
// Messages are concatenated and sorted by timestamp.
// StartTime and EndTime are recomputed.
func DeduplicateSessions(sessions []parser.Session) []parser.Session {
	if len(sessions) == 0 {
		return nil
	}

	groups := make(map[string]*parser.Session)
	for i := range sessions {
		s := &sessions[i]
		existing, ok := groups[s.SessionID]
		if !ok {
			cp := *s
			cp.Messages = make([]parser.Message, len(s.Messages))
			copy(cp.Messages, s.Messages)
			groups[s.SessionID] = &cp
			continue
		}
		existing.Messages = append(existing.Messages, s.Messages...)
	}

	result := make([]parser.Session, 0, len(groups))
	for _, sess := range groups {
		sort.Slice(sess.Messages, func(i, j int) bool {
			return sess.Messages[i].Timestamp.Before(sess.Messages[j].Timestamp)
		})
		if len(sess.Messages) > 0 {
			sess.StartTime = sess.Messages[0].Timestamp
			sess.EndTime = sess.Messages[len(sess.Messages)-1].Timestamp
		}
		result = append(result, *sess)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].StartTime.Before(result[j].StartTime)
	})

	return result
}
