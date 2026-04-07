package s3state

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const s3Key = "_state/state.json"

// Config holds S3 connection parameters for state backup.
type Config struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
	UseSSL    bool
}

// Client wraps a minio client for state file backup/restore.
type Client struct {
	mc     *minio.Client
	bucket string
	logger *slog.Logger
}

// NewClient creates a Client for S3 state operations.
func NewClient(cfg Config, logger *slog.Logger) (*Client, error) {
	mc, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("s3state: create client: %w", err)
	}
	return &Client{mc: mc, bucket: cfg.Bucket, logger: logger}, nil
}

// Download fetches the state file from S3 to localPath.
// NoSuchKey is treated as first run (no error). Other errors are logged but not returned.
func (c *Client) Download(ctx context.Context, localPath string) {
	dir := filepath.Dir(localPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		c.logger.Warn("s3state: mkdir for download", "path", dir, "error", err)
		return
	}

	if err := c.mc.FGetObject(ctx, c.bucket, s3Key, localPath, minio.GetObjectOptions{}); err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" {
			c.logger.Info("s3state: no remote state (first run)")
			return
		}
		c.logger.Warn("s3state: download failed", "error", err)
		return
	}
	c.logger.Info("s3state: restored state from S3")
}

// Upload copies the local state file to S3.
// Errors are logged but not returned.
func (c *Client) Upload(ctx context.Context, localPath string) {
	if _, err := os.Stat(localPath); err != nil {
		c.logger.Warn("s3state: local state file not found for upload", "path", localPath, "error", err)
		return
	}

	if _, err := c.mc.FPutObject(ctx, c.bucket, s3Key, localPath, minio.PutObjectOptions{
		ContentType: "application/json",
	}); err != nil {
		c.logger.Warn("s3state: upload failed", "error", err)
		return
	}
	c.logger.Info("s3state: backed up state to S3")
}
