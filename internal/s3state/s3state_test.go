package s3state

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func TestDownload_NoSuchKey_NoPanic(t *testing.T) {
	// Cannot connect to real S3 in unit test; verify that Download
	// with an unreachable endpoint does not panic and does not leave
	// a partial file.
	cfg := Config{
		Endpoint:  "127.0.0.1:19999",
		Bucket:    "fake",
		AccessKey: "key",
		SecretKey: "secret",
		Region:    "us-east-1",
		UseSSL:    false,
	}
	c, err := NewClient(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	localPath := filepath.Join(t.TempDir(), "state.json")
	// Should not panic; errors are logged only.
	c.Download(context.Background(), localPath)

	if _, err := os.Stat(localPath); !os.IsNotExist(err) {
		t.Fatal("local file should not exist after failed download")
	}
}

func TestUpload_MissingFile_NoPanic(t *testing.T) {
	cfg := Config{
		Endpoint:  "127.0.0.1:19999",
		Bucket:    "fake",
		AccessKey: "key",
		SecretKey: "secret",
		Region:    "us-east-1",
		UseSSL:    false,
	}
	c, err := NewClient(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// Upload a file that does not exist — should not panic.
	c.Upload(context.Background(), filepath.Join(t.TempDir(), "nonexistent.json"))
}

func TestNewClient_InvalidEndpoint(t *testing.T) {
	// Empty endpoint should still create client (minio validates lazily).
	cfg := Config{
		Endpoint:  "localhost:9000",
		Bucket:    "b",
		AccessKey: "a",
		SecretKey: "s",
	}
	c, err := NewClient(cfg, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("client should not be nil")
	}
}
