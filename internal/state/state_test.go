package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewStore_EmptyState(t *testing.T) {
	s := NewStore("/tmp/fake.json")
	if s.data.Version != 1 {
		t.Fatalf("want version 1, got %d", s.data.Version)
	}
	if len(s.Files()) != 0 {
		t.Fatalf("want empty files, got %d", len(s.Files()))
	}
}

func TestLoad_MissingFile(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err := s.Load(); err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(s.Files()) != 0 {
		t.Fatalf("want empty files after missing file load")
	}
}

func TestLoad_CorruptedFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(p, []byte("{invalid json"), 0o644)

	s := NewStore(p)
	if err := s.Load(); err == nil {
		t.Fatal("expected error for corrupted file")
	}
}

func TestSaveAndLoad_Roundtrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	s := NewStore(p)

	now := time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC)
	s.MarkProcessed("/a/b.jsonl", FileState{
		ModTime:     now.Add(-time.Hour),
		Size:        1024,
		ProcessedAt: now,
	})

	if err := s.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	s2 := NewStore(p)
	if err := s2.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}

	fs, ok := s2.Files()["/a/b.jsonl"]
	if !ok {
		t.Fatal("file not found after roundtrip")
	}
	if fs.Size != 1024 {
		t.Fatalf("want size 1024, got %d", fs.Size)
	}
	if !fs.ProcessedAt.Equal(now) {
		t.Fatalf("want processed_at %v, got %v", now, fs.ProcessedAt)
	}
}

func TestSave_AtomicNoTmpLeftover(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.json")
	s := NewStore(p)
	s.MarkProcessed("/x.jsonl", FileState{Size: 1})

	if err := s.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	// .tmp file should not remain
	if _, err := os.Stat(p + ".tmp"); !os.IsNotExist(err) {
		t.Fatal(".tmp file should not exist after save")
	}
}

func TestMarkProcessed_OverwritesExisting(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "s.json"))
	s.MarkProcessed("/x.jsonl", FileState{Size: 100})
	s.MarkProcessed("/x.jsonl", FileState{Size: 200})

	if s.Files()["/x.jsonl"].Size != 200 {
		t.Fatal("MarkProcessed should overwrite")
	}
}

func TestSave_CreatesParentDir(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "dir", "state.json")
	s := NewStore(p)
	s.MarkProcessed("/a.jsonl", FileState{Size: 1})

	if err := s.Save(); err != nil {
		t.Fatalf("save should create parent dirs: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("state file should exist: %v", err)
	}
}

func TestSave_ValidJSON(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	s := NewStore(p)
	s.MarkProcessed("/a.jsonl", FileState{Size: 42})
	s.Save()

	raw, _ := os.ReadFile(p)
	var data StateData
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("saved file is not valid JSON: %v", err)
	}
	if data.Version != 1 {
		t.Fatalf("want version 1, got %d", data.Version)
	}
}
