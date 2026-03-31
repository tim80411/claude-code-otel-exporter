package reader

import "time"

// FileEntry represents a JSONL file discovered during scanning.
type FileEntry struct {
	Path        string
	ProjectName string
	ModTime     time.Time
	Size        int64
}

// Reader scans for JSONL session files to process.
type Reader interface {
	Scan() ([]FileEntry, error)
}
