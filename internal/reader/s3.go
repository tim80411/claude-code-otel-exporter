package reader

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/tim80411/claude-code-otel-exporter/internal/state"
)

// S3Reader lists and downloads JSONL files from an S3-compatible bucket.
type S3Reader struct {
	client    *minio.Client
	bucket    string
	tempDir   string
	processed map[string]state.FileState
	logger    *slog.Logger
}

// S3Config holds S3 connection parameters.
type S3Config struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
	UseSSL    bool
}

// NewS3Reader creates an S3Reader and initializes the minio client.
func NewS3Reader(cfg S3Config, processed map[string]state.FileState, logger *slog.Logger) (*S3Reader, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("reader: s3 client: %w", err)
	}

	tempDir, err := os.MkdirTemp("", "otel-s3-*")
	if err != nil {
		return nil, fmt.Errorf("reader: create temp dir: %w", err)
	}

	return &S3Reader{
		client:    client,
		bucket:    cfg.Bucket,
		tempDir:   tempDir,
		processed: processed,
		logger:    logger,
	}, nil
}

// TempDir returns the temp directory for cleanup after processing.
func (r *S3Reader) TempDir() string {
	return r.tempDir
}

// Scan lists .jsonl objects in the bucket, downloads new/modified ones,
// and returns FileEntry pointing to the local temp files.
func (r *S3Reader) Scan() ([]FileEntry, error) {
	ctx := context.Background()
	var entries []FileEntry

	for obj := range r.client.ListObjects(ctx, r.bucket, minio.ListObjectsOptions{Recursive: true}) {
		if obj.Err != nil {
			return nil, fmt.Errorf("reader: s3 list: %w", obj.Err)
		}

		if !strings.HasSuffix(obj.Key, ".jsonl") {
			continue
		}

		// Check against processed state using S3 key as the path.
		if prev, ok := r.processed[obj.Key]; ok {
			if !obj.LastModified.After(prev.ModTime) {
				continue
			}
		}

		// Download to temp file.
		localPath := filepath.Join(r.tempDir, sanitizeKey(obj.Key))
		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
			return nil, fmt.Errorf("reader: mkdir for %s: %w", obj.Key, err)
		}

		if err := r.client.FGetObject(ctx, r.bucket, obj.Key, localPath, minio.GetObjectOptions{}); err != nil {
			r.logger.Warn("s3 download failed", "key", obj.Key, "error", err)
			continue
		}

		entries = append(entries, FileEntry{
			Path:        obj.Key, // Use S3 key for state tracking
			ProjectName: extractProjectFromS3Key(obj.Key),
			ModTime:     obj.LastModified,
			Size:        obj.Size,
		})

		r.logger.Debug("downloaded s3 object", "key", obj.Key, "size", obj.Size)
	}

	return entries, nil
}

// LocalPath returns the local temp file path for an S3 key.
func (r *S3Reader) LocalPath(s3Key string) string {
	return filepath.Join(r.tempDir, sanitizeKey(s3Key))
}

// sanitizeKey converts an S3 key to a safe local file path.
func sanitizeKey(key string) string {
	return strings.ReplaceAll(key, "/", string(filepath.Separator))
}

// extractProjectFromS3Key extracts the project name from an S3 object key.
// Expected patterns:
//   - user-tim/projects/%2FUsers%2Ftim%2Frepo-a/sessions/abc.jsonl
//   - projects/%2FUsers%2Ftim%2Frepo-a/abc.jsonl
func extractProjectFromS3Key(key string) string {
	parts := strings.Split(key, "/")
	for i, part := range parts {
		if part == "projects" && i+1 < len(parts) {
			decoded, err := url.PathUnescape(parts[i+1])
			if err != nil {
				return parts[i+1]
			}
			return decoded
		}
	}
	// Fallback: use the directory containing the file.
	if len(parts) >= 2 {
		return parts[len(parts)-2]
	}
	return ""
}
