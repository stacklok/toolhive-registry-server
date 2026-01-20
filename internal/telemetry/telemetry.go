package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Telemetry encapsulates OpenTelemetry providers and handles their lifecycle.
// It provides a unified interface for initializing and shutting down telemetry.
type Telemetry struct {
	tracerProvider trace.TracerProvider
	meterProvider  metric.MeterProvider
}

// Option is a function that configures the telemetry setup
type Option func(*telemetryConfig)

// telemetryConfig holds the configuration for creating telemetry
type telemetryConfig struct {
	config *Config
}

// WithTelemetryConfig sets the telemetry configuration
func WithTelemetryConfig(cfg *Config) Option {
	return func(tc *telemetryConfig) {
		tc.config = cfg
	}
}

// New creates and initializes a new Telemetry instance based on the configuration.
// If telemetry is disabled or configuration is nil, returns a Telemetry with no-op providers.
// The caller is responsible for calling Shutdown when the application exits.
func New(ctx context.Context, opts ...Option) (*Telemetry, error) {
	cfg := &telemetryConfig{}

	for _, opt := range opts {
		opt(cfg)
	}

	// Return no-op telemetry if config is nil or disabled
	if cfg.config == nil || !cfg.config.Enabled {
		slog.Debug("Telemetry disabled")
		return newNoOpTelemetry(ctx)
	}

	// Validate configuration
	if err := cfg.config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid telemetry configuration: %w", err)
	}

	slog.Info("Initializing telemetry",
		"service_name", cfg.config.GetServiceName(),
		"service_version", cfg.config.GetServiceVersion(),
	)

	// Create tracer provider
	tracerProvider, err := NewTracerProvider(ctx,
		WithTracerServiceName(cfg.config.GetServiceName()),
		WithTracerServiceVersion(cfg.config.GetServiceVersion()),
		WithTracingConfig(cfg.config.Tracing),
		WithTracerEndpoint(cfg.config.GetEndpoint()),
		WithTracerInsecure(cfg.config.GetInsecure()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tracer provider: %w", err)
	}

	// Create meter provider
	meterProvider, err := NewMeterProvider(ctx,
		WithMeterServiceName(cfg.config.GetServiceName()),
		WithMeterServiceVersion(cfg.config.GetServiceVersion()),
		WithMetricsConfig(cfg.config.Metrics),
		WithMeterEndpoint(cfg.config.GetEndpoint()),
		WithMeterInsecure(cfg.config.GetInsecure()),
	)
	if err != nil {
		// Clean up tracer provider if meter provider creation fails
		if shutdownable, ok := tracerProvider.(*sdktrace.TracerProvider); ok {
			_ = shutdownable.Shutdown(ctx)
		}
		return nil, fmt.Errorf("failed to create meter provider: %w", err)
	}

	slog.Info("Telemetry initialized successfully")

	return &Telemetry{
		tracerProvider: tracerProvider,
		meterProvider:  meterProvider,
	}, nil
}

// newNoOpTelemetry creates a Telemetry instance with no-op providers
func newNoOpTelemetry(ctx context.Context) (*Telemetry, error) {
	tracerProvider, err := NewTracerProvider(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create no-op tracer provider: %w", err)
	}

	meterProvider, err := NewMeterProvider(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create no-op meter provider: %w", err)
	}

	return &Telemetry{
		tracerProvider: tracerProvider,
		meterProvider:  meterProvider,
	}, nil
}

// TracerProvider returns the configured tracer provider
func (t *Telemetry) TracerProvider() trace.TracerProvider {
	return t.tracerProvider
}

// MeterProvider returns the configured meter provider
func (t *Telemetry) MeterProvider() metric.MeterProvider {
	return t.meterProvider
}

// Tracer returns a named tracer from the tracer provider
func (t *Telemetry) Tracer(name string, opts ...trace.TracerOption) trace.Tracer {
	return t.tracerProvider.Tracer(name, opts...)
}

// Meter returns a named meter from the meter provider
func (t *Telemetry) Meter(name string, opts ...metric.MeterOption) metric.Meter {
	return t.meterProvider.Meter(name, opts...)
}

// Shutdown gracefully shuts down all telemetry providers.
// It should be called when the application is shutting down to flush any pending telemetry data.
// This method is safe to call multiple times.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	slog.Info("Shutting down telemetry")

	var errs []error

	// Shutdown tracer provider if it's an SDK provider
	if tp, ok := t.tracerProvider.(*sdktrace.TracerProvider); ok {
		if err := tp.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to shutdown tracer provider: %w", err))
		} else {
			slog.Debug("Tracer provider shutdown complete")
		}
	}

	// Shutdown meter provider if it's an SDK provider
	if mp, ok := t.meterProvider.(*sdkmetric.MeterProvider); ok {
		if err := mp.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to shutdown meter provider: %w", err))
		} else {
			slog.Debug("Meter provider shutdown complete")
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	slog.Info("Telemetry shutdown complete")
	return nil
}
