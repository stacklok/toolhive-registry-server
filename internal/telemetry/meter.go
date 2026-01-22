package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

const (
	// DefaultMetricsInterval is the default interval for metric collection
	DefaultMetricsInterval = 60 * time.Second
)

// MeterProviderOption is a function that configures the meter provider setup
type MeterProviderOption func(*meterProviderConfig)

// meterProviderConfig holds the configuration for creating a meter provider
type meterProviderConfig struct {
	serviceName    string
	serviceVersion string
	metricsConfig  *MetricsConfig
	endpoint       string
	insecure       bool
}

// WithMeterServiceName sets the service name for the meter provider
func WithMeterServiceName(name string) MeterProviderOption {
	return func(cfg *meterProviderConfig) {
		cfg.serviceName = name
	}
}

// WithMeterServiceVersion sets the service version for the meter provider
func WithMeterServiceVersion(version string) MeterProviderOption {
	return func(cfg *meterProviderConfig) {
		cfg.serviceVersion = version
	}
}

// WithMetricsConfig sets the metrics configuration
func WithMetricsConfig(mc *MetricsConfig) MeterProviderOption {
	return func(cfg *meterProviderConfig) {
		cfg.metricsConfig = mc
	}
}

// WithMeterEndpoint sets the endpoint for the meter provider
func WithMeterEndpoint(endpoint string) MeterProviderOption {
	return func(cfg *meterProviderConfig) {
		cfg.endpoint = endpoint
	}
}

// WithMeterInsecure sets the insecure flag for the meter provider
func WithMeterInsecure(insecure bool) MeterProviderOption {
	return func(cfg *meterProviderConfig) {
		cfg.insecure = insecure
	}
}

// NewMeterProvider creates a new OpenTelemetry MeterProvider based on the configuration.
// Returns a no-op provider if metrics are disabled or configuration is nil.
// The caller is responsible for calling Shutdown on the returned provider.
func NewMeterProvider(ctx context.Context, opts ...MeterProviderOption) (metric.MeterProvider, error) {
	cfg := &meterProviderConfig{
		serviceName:    DefaultServiceName,
		serviceVersion: "unknown",
		endpoint:       DefaultEndpoint,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	// Return no-op provider if metrics are disabled
	if cfg.metricsConfig == nil || !cfg.metricsConfig.Enabled {
		slog.Info("Metrics disabled, using no-op meter provider")
		return noop.NewMeterProvider(), nil
	}

	// Create resource with service information
	// We use resource.New to avoid schema URL conflicts with resource.Default()
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
	exporter, err := createOTLPMetricsExporter(ctx, cfg.endpoint, cfg.insecure)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP metrics exporter: %w", err)
	}

	// Create meter provider with periodic reader
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(
				exporter,
				sdkmetric.WithInterval(DefaultMetricsInterval),
			),
		),
	)

	// Set as global meter provider
	otel.SetMeterProvider(mp)

	slog.Info("Metrics initialized",
		"endpoint", cfg.endpoint,
		"insecure", cfg.insecure,
	)

	return mp, nil
}

// createOTLPMetricsExporter creates an OTLP HTTP metric exporter
func createOTLPMetricsExporter(ctx context.Context, endpoint string, insecure bool) (sdkmetric.Exporter, error) {
	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(endpoint),
	}

	if insecure {
		opts = append(opts, otlpmetrichttp.WithInsecure())
	}

	exporter, err := otlpmetrichttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP metric exporter: %w", err)
	}

	return exporter, nil
}
