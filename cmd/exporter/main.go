package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"fmt"

	"github.com/tim80411/claude-code-otel-exporter/internal/backfill"
	"github.com/tim80411/claude-code-otel-exporter/internal/config"
	"github.com/tim80411/claude-code-otel-exporter/internal/events"
	"github.com/tim80411/claude-code-otel-exporter/internal/metrics"
	"github.com/tim80411/claude-code-otel-exporter/internal/parser"
	"github.com/tim80411/claude-code-otel-exporter/internal/reader"
	"github.com/tim80411/claude-code-otel-exporter/internal/retry"
	"github.com/tim80411/claude-code-otel-exporter/internal/s3state"
	"github.com/tim80411/claude-code-otel-exporter/internal/state"
)

type PipelineResult struct {
	FilesProcessed   int
	SessionsExported int
	ParseErrors      int
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, jsonHandlerOpts(slog.LevelInfo))).
			Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)
	logger.Info("config loaded", cfg.LogFields()...)

	if err := cfg.Preflight(); err != nil {
		logger.Error("preflight check failed", "error", err)
		os.Exit(1)
	}
	logger.Info("preflight checks passed")

	start := time.Now()
	result, err := runPipeline(context.Background(), cfg, logger)
	if err != nil {
		logger.Error("pipeline failed", "error", err)
		os.Exit(1)
	}

	logger.Info("run complete",
		"files_processed", result.FilesProcessed,
		"sessions_exported", result.SessionsExported,
		"elapsed_ms", time.Since(start).Milliseconds(),
	)
}

func runPipeline(ctx context.Context, cfg *config.Config, log *slog.Logger) (PipelineResult, error) {
	// 0. If local state file is missing and S3 config is available, restore from S3
	var s3StateClient *s3state.Client
	if cfg.S3Endpoint != "" && cfg.S3Bucket != "" && cfg.S3AccessKey != "" && cfg.S3SecretKey != "" {
		c, err := s3state.NewClient(s3state.Config{
			Endpoint:  cfg.S3Endpoint,
			Bucket:    cfg.S3Bucket,
			AccessKey: cfg.S3AccessKey,
			SecretKey: cfg.S3SecretKey,
			Region:    cfg.S3Region,
			UseSSL:    cfg.S3UseSSL,
		}, log)
		if err != nil {
			log.Warn("s3state client creation failed, continuing without S3 state backup", "error", err)
		} else {
			s3StateClient = c
			if _, err := os.Stat(cfg.StateFilePath); os.IsNotExist(err) {
				log.Info("local state file missing, attempting S3 restore")
				s3StateClient.Download(ctx, cfg.StateFilePath)
			}
		}
	}

	// 1. Load state
	store := state.NewStore(cfg.StateFilePath)
	if err := store.Load(); err != nil {
		return PipelineResult{}, err
	}

	// 2. Create reader based on data source
	var (
		r        reader.Reader
		s3Reader *reader.S3Reader // kept for temp cleanup
		readerErr error
	)
	switch cfg.DataSource {
	case "s3":
		s3Reader, readerErr = reader.NewS3Reader(reader.S3Config{
			Endpoint:  cfg.S3Endpoint,
			Bucket:    cfg.S3Bucket,
			AccessKey: cfg.S3AccessKey,
			SecretKey: cfg.S3SecretKey,
			Region:    cfg.S3Region,
			UseSSL:    cfg.S3UseSSL,
		}, store.Files(), log)
		if readerErr != nil {
			return PipelineResult{}, readerErr
		}
		defer os.RemoveAll(s3Reader.TempDir())
		r = s3Reader
	default:
		r, readerErr = reader.NewLocalReader(cfg.SourceDir, store.Files(), log)
		if readerErr != nil {
			return PipelineResult{}, readerErr
		}
	}

	// 3. Scan for new/changed files
	files, err := r.Scan()
	if err != nil {
		return PipelineResult{}, err
	}

	// 4. No new files → early return
	if len(files) == 0 {
		log.Info("no new files to process")
		return PipelineResult{}, nil
	}

	log.Info("files to process", "count", len(files))

	// 5. Parse each file
	var allSessions []parser.Session
	var parseErrors int
	for _, f := range files {
		// For S3, use temp file path; for local, Path is already absolute.
		openPath := f.Path
		if s3Reader != nil {
			openPath = s3Reader.LocalPath(f.Path)
		}
		file, err := os.Open(openPath)
		if err != nil {
			log.Warn("skipping file", "path", f.Path, "error", err)
			parseErrors++
			continue
		}
		sessions, err := parser.Parse(file, log)
		file.Close()
		if err != nil {
			log.Warn("parse failed", "path", f.Path, "error", err)
			parseErrors++
			continue
		}
		for i := range sessions {
			sessions[i].ProjectName = f.ProjectName
		}
		allSessions = append(allSessions, sessions...)
	}

	log.Info("parsed sessions", "count", len(allSessions))

	// 6-9. Deduplicate and export metrics.
	uniqueSessions := metrics.DeduplicateSessions(allSessions)
	log.Info("unique sessions", "count", len(uniqueSessions))

	// 6-9. Aggregate + Remote Write (per-minute buckets with real timestamps).
	if err := runBackfill(ctx, cfg, log, uniqueSessions); err != nil {
		return PipelineResult{}, err
	}

	// Push events to Loki (if configured) — both modes.
	if err := pushEvents(ctx, cfg, log, uniqueSessions); err != nil {
		return PipelineResult{}, err
	}

	// 10. Mark all files as processed
	now := time.Now().UTC()
	for _, f := range files {
		store.MarkProcessed(f.Path, state.FileState{
			ModTime:     f.ModTime,
			Size:        f.Size,
			ProcessedAt: now,
		})
	}

	// 11. Save state
	if err := store.Save(); err != nil {
		return PipelineResult{}, err
	}

	// 12. Backup state to S3
	if s3StateClient != nil {
		s3StateClient.Upload(ctx, cfg.StateFilePath)
	}

	return PipelineResult{FilesProcessed: len(files), SessionsExported: len(uniqueSessions), ParseErrors: parseErrors}, nil
}

func runBackfill(ctx context.Context, cfg *config.Config, log *slog.Logger, sessions []parser.Session) error {
	buckets := backfill.Aggregate(sessions, time.Minute)
	series := backfill.BuildTimeSeries(buckets, cfg.ServiceName)
	log.Info("backfill: time series built", "buckets", len(buckets), "series", len(series))

	writer := backfill.NewWriter(cfg.RemoteWriteEndpoint, cfg.RemoteWriteAuth, log)
	if err := retry.Do(ctx, cfg.ExportMaxRetries, "backfill-remote-write", log, func() error {
		rCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		return writer.Write(rCtx, series)
	}); err != nil {
		return fmt.Errorf("backfill: remote write: %w", err)
	}
	log.Info("backfill: metrics pushed")
	return nil
}

func pushEvents(ctx context.Context, cfg *config.Config, log *slog.Logger, sessions []parser.Session) error {
	if cfg.LokiEndpoint == "" {
		return nil
	}

	var allEvents []events.Event
	for _, sess := range sessions {
		allEvents = append(allEvents, events.ExtractEvents(sess)...)
	}
	log.Info("events extracted", "count", len(allEvents))

	loki := events.NewLokiClient(cfg.LokiEndpoint, cfg.LokiBasicAuth, log)
	if err := retry.Do(ctx, cfg.ExportMaxRetries, "loki-push", log, func() error {
		eCtx, eCancel := context.WithTimeout(ctx, 30*time.Second)
		defer eCancel()
		return loki.Push(eCtx, allEvents)
	}); err != nil {
		return fmt.Errorf("pipeline: push events: %w", err)
	}
	log.Info("events pushed to loki")
	return nil
}

func newLogger(level string) *slog.Logger {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "info":
		l = slog.LevelInfo
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, jsonHandlerOpts(l)))
}

func jsonHandlerOpts(level slog.Level) *slog.HandlerOptions {
	return &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			switch a.Key {
			case slog.TimeKey:
				a.Key = "timestamp"
			case slog.MessageKey:
				a.Key = "message"
			}
			return a
		},
	}
}
