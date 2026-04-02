package exporter

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"

	"github.com/tim80411/claude-code-otel-exporter/internal/config"
)

// Provider wraps the OTEL SDK MeterProvider and manages its lifecycle.
type Provider struct {
	mp *sdkmetric.MeterProvider
}

// Option configures the Provider.
type Option func(*options)

type options struct {
	reader sdkmetric.Reader // nil = build PeriodicReader from gRPC config
}

// WithReader overrides the default PeriodicReader+gRPC exporter.
// Use with sdkmetric.NewManualReader() in tests.
func WithReader(r sdkmetric.Reader) Option {
	return func(o *options) { o.reader = r }
}

// New creates a Provider with an OTLP gRPC metric exporter.
// When opts includes WithReader, the gRPC connection is skipped.
func New(ctx context.Context, cfg *config.Config, log *slog.Logger, opts ...Option) (*Provider, error) {
	var o options
	for _, fn := range opts {
		fn(&o)
	}

	reader := o.reader
	if reader == nil {
		grpcOpts := []otlpmetricgrpc.Option{
			otlpmetricgrpc.WithEndpoint(cfg.CollectorEndpoint),
		}
		if cfg.CollectorInsecure {
			grpcOpts = append(grpcOpts, otlpmetricgrpc.WithInsecure())
		}

		exp, err := otlpmetricgrpc.New(ctx, grpcOpts...)
		if err != nil {
			return nil, fmt.Errorf("exporter: connect gRPC: %w", err)
		}
		log.Info("OTLP gRPC exporter connected", "endpoint", cfg.CollectorEndpoint, "insecure", cfg.CollectorInsecure)

		reader = sdkmetric.NewPeriodicReader(exp)
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
		),
	)
	if err != nil {
		if o.reader == nil {
			_ = reader.Shutdown(ctx)
		}
		return nil, fmt.Errorf("exporter: build resource: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(reader),
	)

	return &Provider{mp: mp}, nil
}

// MeterProvider returns the underlying SDK MeterProvider.
// TIM-124 uses this to create metric instruments.
func (p *Provider) MeterProvider() *sdkmetric.MeterProvider {
	return p.mp
}

// ForceFlush exports all pending metrics immediately.
func (p *Provider) ForceFlush(ctx context.Context) error {
	if err := p.mp.ForceFlush(ctx); err != nil {
		return fmt.Errorf("exporter: force flush: %w", err)
	}
	return nil
}

// Shutdown flushes remaining metrics and releases resources.
// Call exactly once at process exit.
func (p *Provider) Shutdown(ctx context.Context) error {
	if err := p.mp.Shutdown(ctx); err != nil {
		return fmt.Errorf("exporter: shutdown: %w", err)
	}
	return nil
}
