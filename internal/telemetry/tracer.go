package telemetry

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// TracerProviderOption is a function that configures the tracer provider setup
type TracerProviderOption func(*tracerProviderConfig)

// tracerProviderConfig holds the configuration for creating a tracer provider
type tracerProviderConfig struct {
	serviceName    string
	serviceVersion string
	tracingConfig  *TracingConfig
	endpoint       string
	insecure       bool
}

// WithTracerServiceName sets the service name for the tracer provider
func WithTracerServiceName(name string) TracerProviderOption {
	return func(cfg *tracerProviderConfig) {
		cfg.serviceName = name
	}
}

// WithTracerServiceVersion sets the service version for the tracer provider
func WithTracerServiceVersion(version string) TracerProviderOption {
	return func(cfg *tracerProviderConfig) {
		cfg.serviceVersion = version
	}
}

// WithTracingConfig sets the tracing configuration
func WithTracingConfig(tc *TracingConfig) TracerProviderOption {
	return func(cfg *tracerProviderConfig) {
		cfg.tracingConfig = tc
	}
}

// WithTracerEndpoint sets the endpoint for the tracer provider
func WithTracerEndpoint(endpoint string) TracerProviderOption {
	return func(cfg *tracerProviderConfig) {
		cfg.endpoint = endpoint
	}
}

// WithTracerInsecure sets the insecure flag for the tracer provider
func WithTracerInsecure(insecure bool) TracerProviderOption {
	return func(cfg *tracerProviderConfig) {
		cfg.insecure = insecure
	}
}

// NewTracerProvider creates a new OpenTelemetry TracerProvider based on the configuration.
// Returns a no-op provider if tracing is disabled or configuration is nil.
// The caller is responsible for calling Shutdown on the returned provider.
func NewTracerProvider(ctx context.Context, opts ...TracerProviderOption) (trace.TracerProvider, error) {
	cfg := &tracerProviderConfig{
		serviceName:    DefaultServiceName,
		serviceVersion: "unknown",
		endpoint:       DefaultEndpoint,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	// Return no-op provider if tracing is disabled
	if cfg.tracingConfig == nil || !cfg.tracingConfig.Enabled {
		slog.Info("Tracing disabled, using no-op tracer provider")
		return noop.NewTracerProvider(), nil
	}

	// Create resource with service information
	// We use NewSchemaless to avoid schema URL conflicts with resource.Default()
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.serviceName),
			semconv.ServiceVersion(cfg.serviceVersion),
		),
		resource.WithHost(),
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create OTLP exporter
	exporter, err := createOTLPTracingExporter(ctx, cfg.endpoint, cfg.insecure)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP tracing exporter: %w", err)
	}

	// Create tracer provider with batch span processor
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.tracingConfig.GetSampling())),
	)

	// Set as global tracer provider
	otel.SetTracerProvider(tp)

	// Set global propagator for W3C Trace Context propagation
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	slog.Info("W3C Trace Context propagator configured")

	if cfg.insecure {
		slog.Warn("Tracing configured with insecure connection - telemetry data will be transmitted over unencrypted HTTP. This should only be used in development/testing environments.")
	}

	slog.Info("Tracing initialized",
		"endpoint", cfg.endpoint,
		"sampling_ratio", cfg.tracingConfig.GetSampling(),
		"insecure", cfg.insecure,
	)

	return tp, nil
}

// createOTLPTracingExporter creates an OTLP HTTP trace exporter
func createOTLPTracingExporter(ctx context.Context, endpoint string, insecure bool) (sdktrace.SpanExporter, error) {
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(endpoint),
	}

	if insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}

	return exporter, nil
}
