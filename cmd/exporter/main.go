package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/tim80411/claude-code-otel-exporter/internal/config"
	"github.com/tim80411/claude-code-otel-exporter/internal/parser"
	"github.com/tim80411/claude-code-otel-exporter/internal/reader"
	"github.com/tim80411/claude-code-otel-exporter/internal/state"
)

type PipelineResult struct {
	FilesProcessed   int
	SessionsExported int
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

func runPipeline(_ context.Context, cfg *config.Config, log *slog.Logger) (PipelineResult, error) {
	// 1. Load state
	store := state.NewStore(cfg.StateFilePath)
	if err := store.Load(); err != nil {
		return PipelineResult{}, err
	}

	// 2. Create reader
	lr, err := reader.NewLocalReader(cfg.SourceDir, store.Files(), log)
	if err != nil {
		return PipelineResult{}, err
	}

	// 3. Scan for new/changed files
	files, err := lr.Scan()
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
	for _, f := range files {
		file, err := os.Open(f.Path)
		if err != nil {
			log.Warn("skipping file", "path", f.Path, "error", err)
			continue
		}
		sessions, err := parser.Parse(file, log)
		file.Close()
		if err != nil {
			log.Warn("parse failed", "path", f.Path, "error", err)
			continue
		}
		for i := range sessions {
			sessions[i].ProjectName = f.ProjectName
		}
		allSessions = append(allSessions, sessions...)
	}

	log.Info("parsed sessions", "count", len(allSessions))

	// 6. [Story 3+ placeholder: export sessions via OTLP]

	// 7. Mark all files as processed
	now := time.Now().UTC()
	for _, f := range files {
		store.MarkProcessed(f.Path, state.FileState{
			ModTime:     f.ModTime,
			Size:        f.Size,
			ProcessedAt: now,
		})
	}

	// 8. Save state
	if err := store.Save(); err != nil {
		return PipelineResult{}, err
	}

	return PipelineResult{FilesProcessed: len(files)}, nil
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
