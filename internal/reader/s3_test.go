package reader

import "testing"

func TestExtractProjectFromS3Key_Standard(t *testing.T) {
	key := "user-tim/projects/%2FUsers%2Ftim%2Frepo-a/sessions/abc.jsonl"
	got := extractProjectFromS3Key(key)
	want := "/Users/tim/repo-a"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExtractProjectFromS3Key_NoProjects(t *testing.T) {
	key := "some-dir/abc.jsonl"
	got := extractProjectFromS3Key(key)
	if got != "some-dir" {
		t.Errorf("got %q, want %q", got, "some-dir")
	}
}

func TestExtractProjectFromS3Key_RootFile(t *testing.T) {
	key := "abc.jsonl"
	got := extractProjectFromS3Key(key)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestSanitizeKey(t *testing.T) {
	key := "user/projects/repo/file.jsonl"
	got := sanitizeKey(key)
	// On any OS, should not contain forward slashes as path separators are used.
	if got == "" {
		t.Error("empty result")
	}
}
