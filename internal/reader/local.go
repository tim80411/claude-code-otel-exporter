package reader

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tim80411/claude-code-otel-exporter/internal/state"
)

// LocalReader scans a local directory tree for JSONL session files,
// returning only files that are new or modified since last processing.
type LocalReader struct {
	sourceDir string
	processed map[string]state.FileState
	logger    *slog.Logger
}

// NewLocalReader creates a LocalReader after validating that sourceDir exists.
func NewLocalReader(sourceDir string, processed map[string]state.FileState, logger *slog.Logger) (*LocalReader, error) {
	info, err := os.Stat(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("reader: source dir %q: %w", sourceDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("reader: source dir %q: not a directory", sourceDir)
	}
	return &LocalReader{
		sourceDir: sourceDir,
		processed: processed,
		logger:    logger,
	}, nil
}

// Scan walks the source directory, filters for .jsonl files (skipping memory/ dirs),
// and returns entries whose ModTime is newer than state or not yet in state.
// Results are sorted by ModTime ascending.
func (r *LocalReader) Scan() ([]FileEntry, error) {
	var entries []FileEntry

	err := filepath.WalkDir(r.sourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			r.logger.Warn("walk error", "path", path, "error", err)
			return nil // skip inaccessible entries
		}

		// Skip memory/ directories entirely.
		if d.IsDir() && d.Name() == "memory" {
			return filepath.SkipDir
		}

		if d.IsDir() {
			return nil
		}

		// Only .jsonl files.
		if !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			r.logger.Warn("stat error", "path", path, "error", err)
			return nil
		}

		absPath, err := filepath.Abs(path)
		if err != nil {
			return nil
		}

		// Check against processed state — include if new or modtime is newer.
		if prev, ok := r.processed[absPath]; ok {
			if !info.ModTime().After(prev.ModTime) {
				return nil // unchanged
			}
		}

		entries = append(entries, FileEntry{
			Path:        absPath,
			ProjectName: ExtractProjectName(r.sourceDir, absPath),
			ModTime:     info.ModTime(),
			Size:        info.Size(),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reader: walk %s: %w", r.sourceDir, err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ModTime.Before(entries[j].ModTime)
	})
	return entries, nil
}
