package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	promclient "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	coremetrics "github.com/stacklok/toolhive-core/telemetry/metrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	promexporter "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// DefaultMetricsInterval is the default interval for metric collection
const DefaultMetricsInterval = 60 * time.Second

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
//
// When metrics are enabled, a Prometheus reader is attached alongside the OTLP
// reader; the returned http.Handler serves the promoted /metrics surface. The
// handler is nil for a no-op provider.
func NewMeterProvider(ctx context.Context, opts ...MeterProviderOption) (metric.MeterProvider, http.Handler, error) {
	cfg := &meterProviderConfig{
		serviceName:    DefaultServiceName,
		serviceVersion: DefaultServiceVersion,
		endpoint:       DefaultEndpoint,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	// Return no-op provider if metrics are disabled
	if cfg.metricsConfig == nil || !cfg.metricsConfig.Enabled {
		slog.Info("Metrics disabled, using no-op meter provider")
		return noop.NewMeterProvider(), nil, nil
	}

	// Create resource with service information
	// We use resource.New to avoid schema URL conflicts with resource.Default().
	// The stacklok.component/stacklok.product ownership attributes (D8) are
	// merged last so they cannot be spoofed via OTEL_RESOURCE_ATTRIBUTES.
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.serviceName),
			semconv.ServiceVersion(cfg.serviceVersion),
			attribute.String(coremetrics.AttrStacklokComponent, ComponentRegistry),
			attribute.String(coremetrics.AttrStacklokProduct, coremetrics.ProductStacklokPlatform),
		),
		resource.WithHost(),
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create OTLP exporter
	exporter, err := createOTLPMetricsExporter(ctx, cfg.endpoint, cfg.insecure)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create OTLP metrics exporter: %w", err)
	}

	// Create the Prometheus reader, promoting the two ownership attributes to
	// per-series constant labels (D8); host/process/env attributes stay in
	// target_info via the allow filter.
	promReader, promHandler, err := newPrometheusReader()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Prometheus metrics reader: %w", err)
	}

	// Create meter provider with periodic OTLP reader plus the Prometheus reader
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(
				exporter,
				sdkmetric.WithInterval(DefaultMetricsInterval),
			),
		),
		sdkmetric.WithReader(promReader),
	)

	// Set as global meter provider
	otel.SetMeterProvider(mp)

	slog.Info("Metrics initialized",
		"endpoint", cfg.endpoint,
		"insecure", cfg.insecure,
	)

	return mp, promHandler, nil
}

// newPrometheusReader builds a Prometheus exporter (which is itself an
// sdkmetric.Reader) that promotes the stacklok.component/stacklok.product
// resource attributes to per-series constant labels (D8), plus an HTTP handler
// that serves its registry.
func newPrometheusReader() (sdkmetric.Reader, http.Handler, error) {
	registry := promclient.NewRegistry()

	promExp, err := promexporter.New(
		promexporter.WithRegisterer(registry),
		promexporter.WithResourceAsConstantLabels(attribute.NewAllowKeysFilter(
			coremetrics.AttrStacklokComponent, coremetrics.AttrStacklokProduct,
		)),
	)
	if err != nil {
		return nil, nil, err
	}

	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
	})

	return promExp, handler, nil
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
