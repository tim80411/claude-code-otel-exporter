package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FileState records the processing state of a single file.
type FileState struct {
	ModTime     time.Time `json:"mod_time"`
	Size        int64     `json:"size"`
	ProcessedAt time.Time `json:"processed_at"`
}

// StateData is the top-level structure persisted to disk.
type StateData struct {
	Version int                  `json:"version"`
	Files   map[string]FileState `json:"files"`
}

// Store manages incremental processing state backed by a JSON file.
type Store struct {
	path string
	data StateData
}

// NewStore creates a Store that reads/writes state at the given path.
func NewStore(path string) *Store {
	return &Store{
		path: path,
		data: StateData{
			Version: 1,
			Files:   make(map[string]FileState),
		},
	}
}

// Load reads state from disk. A missing file is treated as first run (empty state, no error).
func (s *Store) Load() error {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // first run
		}
		return fmt.Errorf("state: read %s: %w", s.path, err)
	}

	var data StateData
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("state: parse %s: %w", s.path, err)
	}
	if data.Files == nil {
		data.Files = make(map[string]FileState)
	}
	s.data = data
	return nil
}

// Files returns the current file state map.
func (s *Store) Files() map[string]FileState {
	return s.data.Files
}

// MarkProcessed records that a file has been processed.
func (s *Store) MarkProcessed(path string, fs FileState) {
	s.data.Files[path] = fs
}

// Save writes state to disk atomically (write tmp + rename).
func (s *Store) Save() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("state: marshal: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("state: mkdir %s: %w", dir, err)
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("state: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("state: rename %s → %s: %w", tmp, s.path, err)
	}
	return nil
}
