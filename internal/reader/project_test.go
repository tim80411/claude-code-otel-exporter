package reader

import "testing"

func TestExtractProjectName_Standard(t *testing.T) {
	got := ExtractProjectName("/home/.claude/projects", "/home/.claude/projects/-Users-tim80411-myapp/abc.jsonl")
	if got != "Users-tim80411-myapp" {
		t.Fatalf("want Users-tim80411-myapp, got %q", got)
	}
}

func TestExtractProjectName_SessionsSubdir(t *testing.T) {
	got := ExtractProjectName("/home/.claude/projects", "/home/.claude/projects/-Users-tim80411-myapp/sessions/abc.jsonl")
	if got != "Users-tim80411-myapp" {
		t.Fatalf("want Users-tim80411-myapp, got %q", got)
	}
}

func TestExtractProjectName_SubagentPath(t *testing.T) {
	got := ExtractProjectName("/home/.claude/projects", "/home/.claude/projects/-Users-tim80411-myapp/abc-uuid/subagents/agent-1.jsonl")
	if got != "Users-tim80411-myapp" {
		t.Fatalf("want Users-tim80411-myapp, got %q", got)
	}
}

func TestExtractProjectName_NoLeadingDash(t *testing.T) {
	got := ExtractProjectName("/data/projects", "/data/projects/myproject/file.jsonl")
	if got != "myproject" {
		t.Fatalf("want myproject, got %q", got)
	}
}
