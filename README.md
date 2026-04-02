# claude-code-otel-exporter

Parse Claude Code JSONL session files and export as OpenTelemetry metrics to a collector.

## Quick Start

```bash
cp .env.example .env   # edit values
make run               # build and run locally
```

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `SOURCE_DIR` | Yes | — | Path to Claude Code projects directory |
| `STATE_FILE_PATH` | Yes | — | Path to incremental state JSON file |
| `COLLECTOR_ENDPOINT` | Yes | — | OTEL Collector gRPC endpoint (host:port) |
| `SERVICE_NAME` | No | `claude-code-otel-exporter` | OTEL service name |
| `SERVICE_VERSION` | No | `dev` | OTEL service version |
| `COLLECTOR_INSECURE` | No | `false` | Use insecure gRPC connection |
| `LOG_LEVEL` | No | `info` | Log level: debug, info, warn, error |
| `LOKI_ENDPOINT` | No | — | Loki push API URL (enables event push) |
| `LOKI_BASIC_AUTH` | No | — | Base64-encoded Basic auth for Loki |

## Docker

```bash
make docker-build   # build image
make docker-run     # run with local volumes
```

## Development

```bash
make test    # run all tests
make lint    # go vet
make clean   # remove binary
```
