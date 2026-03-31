package reader

import (
	"path/filepath"
	"strings"
)

// ExtractProjectName derives a project name from a file's path relative to sourceDir.
// It takes the first path segment and strips any leading "-".
// Subagent paths (e.g. {UUID}/subagents/agent-*.jsonl) still return the top-level project dir name.
func ExtractProjectName(sourceDir, filePath string) string {
	rel, err := filepath.Rel(sourceDir, filePath)
	if err != nil {
		return ""
	}

	// First path segment is the project directory.
	first := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
	return strings.TrimLeft(first, "-")
}
