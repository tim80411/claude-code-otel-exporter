package reader

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tim80411/claude-code-otel-exporter/internal/state"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

// helper: create a file with the given modtime.
func createFile(t *testing.T, path string, modTime time.Time) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
}

func TestScan_FirstRun(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	createFile(t, filepath.Join(dir, "-proj1", "a.jsonl"), base)
	createFile(t, filepath.Join(dir, "-proj1", "b.jsonl"), base.Add(time.Hour))

	lr, err := NewLocalReader(dir, map[string]state.FileState{}, testLogger)
	if err != nil {
		t.Fatal(err)
	}
	files, err := lr.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("want 2 files, got %d", len(files))
	}
}

func TestScan_Incremental(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	createFile(t, filepath.Join(dir, "-proj", "old.jsonl"), base)
	createFile(t, filepath.Join(dir, "-proj", "new.jsonl"), base.Add(time.Hour))

	oldAbs, _ := filepath.Abs(filepath.Join(dir, "-proj", "old.jsonl"))
	processed := map[string]state.FileState{
		oldAbs: {ModTime: base, Size: 2},
	}

	lr, _ := NewLocalReader(dir, processed, testLogger)
	files, _ := lr.Scan()
	if len(files) != 1 {
		t.Fatalf("want 1 new file, got %d", len(files))
	}
	if files[0].Path != filepath.Join(dir, "-proj", "new.jsonl") {
		// Compare using abs
		newAbs, _ := filepath.Abs(filepath.Join(dir, "-proj", "new.jsonl"))
		if files[0].Path != newAbs {
			t.Fatalf("want new.jsonl, got %s", files[0].Path)
		}
	}
}

func TestScan_UpdatedModTime(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	// File exists in state with old modtime, but actual file has newer modtime.
	createFile(t, filepath.Join(dir, "-proj", "updated.jsonl"), base.Add(2*time.Hour))

	abs, _ := filepath.Abs(filepath.Join(dir, "-proj", "updated.jsonl"))
	processed := map[string]state.FileState{
		abs: {ModTime: base, Size: 2},
	}

	lr, _ := NewLocalReader(dir, processed, testLogger)
	files, _ := lr.Scan()
	if len(files) != 1 {
		t.Fatalf("want 1 updated file, got %d", len(files))
	}
}

func TestNewLocalReader_DirNotExist(t *testing.T) {
	_, err := NewLocalReader("/nonexistent-dir-abc123", map[string]state.FileState{}, testLogger)
	if err == nil {
		t.Fatal("expected error for nonexistent dir")
	}
}

func TestScan_NoJSONL(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	createFile(t, filepath.Join(dir, "-proj", "readme.txt"), base)
	createFile(t, filepath.Join(dir, "-proj", "data.json"), base)

	lr, _ := NewLocalReader(dir, map[string]state.FileState{}, testLogger)
	files, _ := lr.Scan()
	if len(files) != 0 {
		t.Fatalf("want 0 jsonl files, got %d", len(files))
	}
}

func TestScan_SkipsNonJSONL(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	createFile(t, filepath.Join(dir, "-proj", "good.jsonl"), base)
	createFile(t, filepath.Join(dir, "-proj", "bad.txt"), base)
	createFile(t, filepath.Join(dir, "-proj", "also.json"), base)

	lr, _ := NewLocalReader(dir, map[string]state.FileState{}, testLogger)
	files, _ := lr.Scan()
	if len(files) != 1 {
		t.Fatalf("want 1 jsonl, got %d", len(files))
	}
}

func TestScan_SubagentFiles(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	createFile(t, filepath.Join(dir, "-proj", "uuid-123", "subagents", "agent-1.jsonl"), base)

	lr, _ := NewLocalReader(dir, map[string]state.FileState{}, testLogger)
	files, _ := lr.Scan()
	if len(files) != 1 {
		t.Fatalf("want 1 subagent file, got %d", len(files))
	}
	if files[0].ProjectName != "proj" {
		t.Fatalf("want project name 'proj', got %q", files[0].ProjectName)
	}
}

func TestScan_SortedByModTime(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	createFile(t, filepath.Join(dir, "-proj", "c.jsonl"), base.Add(3*time.Hour))
	createFile(t, filepath.Join(dir, "-proj", "a.jsonl"), base)
	createFile(t, filepath.Join(dir, "-proj", "b.jsonl"), base.Add(time.Hour))

	lr, _ := NewLocalReader(dir, map[string]state.FileState{}, testLogger)
	files, _ := lr.Scan()
	if len(files) != 3 {
		t.Fatalf("want 3 files, got %d", len(files))
	}
	for i := 1; i < len(files); i++ {
		if files[i].ModTime.Before(files[i-1].ModTime) {
			t.Fatalf("files not sorted by ModTime: %v before %v at index %d", files[i].ModTime, files[i-1].ModTime, i)
		}
	}
}

func TestScan_SkipsMemoryDir(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	createFile(t, filepath.Join(dir, "-proj", "good.jsonl"), base)
	createFile(t, filepath.Join(dir, "-proj", "memory", "skip.jsonl"), base)

	lr, _ := NewLocalReader(dir, map[string]state.FileState{}, testLogger)
	files, _ := lr.Scan()
	if len(files) != 1 {
		t.Fatalf("want 1 file (skipping memory/), got %d", len(files))
	}
}
