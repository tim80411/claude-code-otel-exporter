package config

import (
	"fmt"
	"net"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	SourceDir         string `envconfig:"SOURCE_DIR"           required:"true"`
	StateFilePath     string `envconfig:"STATE_FILE_PATH"      required:"true"`
	CollectorEndpoint string `envconfig:"COLLECTOR_ENDPOINT"   required:"true"`

	ServiceName    string `envconfig:"SERVICE_NAME"    default:"claude-code-otel-exporter"`
	ServiceVersion string `envconfig:"SERVICE_VERSION" default:"dev"`
	LogLevel       string `envconfig:"LOG_LEVEL"       default:"info"`
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
	return nil
}

func (c *Config) LogFields() []any {
	return []any{
		"source_dir", c.SourceDir,
		"state_file_path", c.StateFilePath,
		"collector_endpoint", c.CollectorEndpoint,
		"service_name", c.ServiceName,
		"service_version", c.ServiceVersion,
		"log_level", c.LogLevel,
	}
}
