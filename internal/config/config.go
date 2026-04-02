package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	SourceDir         string `envconfig:"SOURCE_DIR"`
	StateFilePath     string `envconfig:"STATE_FILE_PATH"      required:"true"`
	CollectorEndpoint string `envconfig:"COLLECTOR_ENDPOINT"   required:"true"`

	ServiceName       string `envconfig:"SERVICE_NAME"       default:"claude-code-otel-exporter"`
	ServiceVersion    string `envconfig:"SERVICE_VERSION"    default:"dev"`
	CollectorInsecure bool   `envconfig:"COLLECTOR_INSECURE" default:"false"`
	LogLevel          string `envconfig:"LOG_LEVEL"          default:"info"`

	LokiEndpoint  string `envconfig:"LOKI_ENDPOINT"`
	LokiBasicAuth string `envconfig:"LOKI_BASIC_AUTH"`

	ExportMaxRetries int `envconfig:"EXPORT_MAX_RETRIES" default:"3"`

	DataSource  string `envconfig:"DATA_SOURCE"   default:"local"`
	S3Endpoint  string `envconfig:"S3_ENDPOINT"`
	S3Bucket    string `envconfig:"S3_BUCKET"`
	S3AccessKey string `envconfig:"S3_ACCESS_KEY"`
	S3SecretKey string `envconfig:"S3_SECRET_KEY"`
	S3Region    string `envconfig:"S3_REGION"     default:"us-east-1"`
	S3UseSSL    bool   `envconfig:"S3_USE_SSL"    default:"true"`
}

func Load() (*Config, error) {
	var c Config
	if err := envconfig.Process("", &c); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

var validLogLevels = map[string]bool{
	"debug": true,
	"info":  true,
	"warn":  true,
	"error": true,
}

func (c *Config) validate() error {
	if _, _, err := net.SplitHostPort(c.CollectorEndpoint); err != nil {
		return fmt.Errorf("config: COLLECTOR_ENDPOINT %q: must be host:port (e.g. otel-collector:4317): %w", c.CollectorEndpoint, err)
	}
	if !validLogLevels[c.LogLevel] {
		return fmt.Errorf("config: LOG_LEVEL %q: must be one of debug, info, warn, error", c.LogLevel)
	}
	switch c.DataSource {
	case "local":
		if c.SourceDir == "" {
			return fmt.Errorf("config: SOURCE_DIR is required when DATA_SOURCE=local")
		}
	case "s3":
		if c.S3Endpoint == "" || c.S3Bucket == "" || c.S3AccessKey == "" || c.S3SecretKey == "" {
			return fmt.Errorf("config: S3_ENDPOINT, S3_BUCKET, S3_ACCESS_KEY, S3_SECRET_KEY are required when DATA_SOURCE=s3")
		}
	default:
		return fmt.Errorf("config: DATA_SOURCE %q: must be one of local, s3", c.DataSource)
	}
	return nil
}

// Preflight checks external service reachability and state path writability.
func (c *Config) Preflight() error {
	// Check collector connectivity.
	conn, err := net.DialTimeout("tcp", c.CollectorEndpoint, 5*time.Second)
	if err != nil {
		return fmt.Errorf("config: COLLECTOR_ENDPOINT %q unreachable: %w", c.CollectorEndpoint, err)
	}
	conn.Close()

	// Check state file parent directory is writable.
	dir := filepath.Dir(c.StateFilePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("config: STATE_FILE_PATH dir %q: %w", dir, err)
	}

	return nil
}

func (c *Config) LogFields() []any {
	fields := []any{
		"source_dir", c.SourceDir,
		"state_file_path", c.StateFilePath,
		"collector_endpoint", c.CollectorEndpoint,
		"service_name", c.ServiceName,
		"service_version", c.ServiceVersion,
		"collector_insecure", c.CollectorInsecure,
		"log_level", c.LogLevel,
	}
	if c.LokiEndpoint != "" {
		fields = append(fields, "loki_endpoint", c.LokiEndpoint)
	}
	return fields
}
