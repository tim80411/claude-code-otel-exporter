package exporter

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/tim80411/claude-code-otel-exporter/internal/config"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

func testConfig() *config.Config {
	return &config.Config{
		CollectorEndpoint: "localhost:4317",
		CollectorInsecure: true,
		ServiceName:       "test-service",
		ServiceVersion:    "0.0.1",
	}
}

func TestNew_WithManualReader(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	ctx := context.Background()

	p, err := New(ctx, testConfig(), testLogger, WithReader(reader))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if p.MeterProvider() == nil {
		t.Fatal("MeterProvider() should not be nil")
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := p.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() error: %v", err)
	}
}

func TestForceFlush_Success(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	ctx := context.Background()

	p, err := New(ctx, testConfig(), testLogger, WithReader(reader))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() {
		_ = p.Shutdown(context.Background())
	}()

	if err := p.ForceFlush(ctx); err != nil {
		t.Fatalf("ForceFlush() error: %v", err)
	}
}

func TestMeterProvider_CreateInstruments(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	ctx := context.Background()

	p, err := New(ctx, testConfig(), testLogger, WithReader(reader))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() {
		_ = p.Shutdown(context.Background())
	}()

	meter := p.MeterProvider().Meter("test")
	counter, err := meter.Int64Counter("test.counter")
	if err != nil {
		t.Fatalf("Int64Counter() error: %v", err)
	}
	counter.Add(ctx, 42)

	// Collect and verify metrics were recorded
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error: %v", err)
	}
	if len(rm.ScopeMetrics) == 0 {
		t.Fatal("expected at least one ScopeMetrics entry")
	}
	if len(rm.ScopeMetrics[0].Metrics) == 0 {
		t.Fatal("expected at least one Metric")
	}
	if rm.ScopeMetrics[0].Metrics[0].Name != "test.counter" {
		t.Fatalf("want metric name test.counter, got %q", rm.ScopeMetrics[0].Metrics[0].Name)
	}
}

func TestResource_ContainsServiceAttributes(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	ctx := context.Background()

	cfg := testConfig()
	cfg.ServiceName = "my-service"
	cfg.ServiceVersion = "1.2.3"

	p, err := New(ctx, cfg, testLogger, WithReader(reader))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() {
		_ = p.Shutdown(context.Background())
	}()

	// Record something to produce ResourceMetrics
	meter := p.MeterProvider().Meter("test")
	counter, _ := meter.Int64Counter("test.counter")
	counter.Add(ctx, 1)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	attrs := rm.Resource.Attributes()
	found := map[string]string{}
	for _, attr := range attrs {
		found[string(attr.Key)] = attr.Value.AsString()
	}

	if found["service.name"] != "my-service" {
		t.Fatalf("want service.name=my-service, got %q", found["service.name"])
	}
	if found["service.version"] != "1.2.3" {
		t.Fatalf("want service.version=1.2.3, got %q", found["service.version"])
	}
}

func TestShutdown_AfterShutdown(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	ctx := context.Background()

	p, err := New(ctx, testConfig(), testLogger, WithReader(reader))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if err := p.Shutdown(ctx); err != nil {
		t.Fatalf("first Shutdown() error: %v", err)
	}

	// Second shutdown should not panic and should return an error
	if err := p.Shutdown(ctx); err == nil {
		t.Error("second Shutdown() should return an error")
	}
}
