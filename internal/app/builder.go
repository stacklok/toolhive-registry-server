// Package app assembles the standalone catalog and owns its application lifecycle.
package app

import (
	"compress/gzip"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	auditmw "github.com/stacklok/toolhive-registry-server/internal/audit"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/telemetry"
)

const (
	defaultHTTPAddress         = ":8080"
	defaultInternalHTTPAddress = ":8081"
	defaultRequestTimeout      = 10 * time.Second
	defaultReadTimeout         = 10 * time.Second
	defaultWriteTimeout        = 15 * time.Second
	defaultIdleTimeout         = 60 * time.Second
)

// RegistryAppOptions configures CatalogApp's HTTP servers.
type RegistryAppOptions func(*registryAppConfig) error

type registryAppConfig struct {
	config                                                 *config.Config
	address, internalAddress                               string
	middlewares                                            []func(http.Handler) http.Handler
	requestTimeout, readTimeout, writeTimeout, idleTimeout time.Duration
	meterProvider                                          metric.MeterProvider
	tracerProvider                                         trace.TracerProvider
	metricsHandler                                         http.Handler
}

// WithAddress sets the public HTTP server address.
func WithAddress(addr string) RegistryAppOptions {
	return func(cfg *registryAppConfig) error {
		if addr == "" {
			return fmt.Errorf("address cannot be empty")
		}
		parts := strings.SplitN(addr, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("address is not a valid port: %s", addr)
		}
		host, port := parts[0], parts[1]
		if port == "" {
			return fmt.Errorf("address is not a valid port: %s", addr)
		}
		if host == "localhost" {
			host = "127.0.0.1"
		}
		if host == "" {
			host = "0.0.0.0"
		}
		if _, err := netip.ParseAddrPort(host + ":" + port); err != nil {
			return fmt.Errorf("address is not a valid port: %w", err)
		}
		cfg.address = addr
		return nil
	}
}

// WithInternalAddress sets the health/readiness/version/metrics server address.
func WithInternalAddress(addr string) RegistryAppOptions {
	return func(cfg *registryAppConfig) error {
		if addr == "" {
			return fmt.Errorf("internal address cannot be empty")
		}
		parts := strings.SplitN(addr, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("internal address is not a valid port: %s", addr)
		}
		host, port := parts[0], parts[1]
		if port == "" {
			return fmt.Errorf("internal address is not a valid port: %s", addr)
		}
		if host == "localhost" {
			host = "127.0.0.1"
		}
		if host == "" {
			host = "0.0.0.0"
		}
		if _, err := netip.ParseAddrPort(host + ":" + port); err != nil {
			return fmt.Errorf("internal address is not a valid port: %w", err)
		}
		cfg.internalAddress = addr
		return nil
	}
}

// WithMiddlewares sets custom HTTP middlewares.
func WithMiddlewares(mw ...func(http.Handler) http.Handler) RegistryAppOptions {
	return func(cfg *registryAppConfig) error { cfg.middlewares = mw; return nil }
}

// WithMeterProvider sets the OpenTelemetry meter provider.
func WithMeterProvider(mp metric.MeterProvider) RegistryAppOptions {
	return func(cfg *registryAppConfig) error { cfg.meterProvider = mp; return nil }
}

// WithTracerProvider sets the OpenTelemetry tracer provider.
func WithTracerProvider(tp trace.TracerProvider) RegistryAppOptions {
	return func(cfg *registryAppConfig) error { cfg.tracerProvider = tp; return nil }
}

// WithMetricsHandler mounts the Prometheus handler on the internal server only.
func WithMetricsHandler(h http.Handler) RegistryAppOptions {
	return func(cfg *registryAppConfig) error { cfg.metricsHandler = h; return nil }
}

func baseConfig(opts ...RegistryAppOptions) (*registryAppConfig, error) {
	cfg := &registryAppConfig{
		address: defaultHTTPAddress, internalAddress: defaultInternalHTTPAddress,
		requestTimeout: defaultRequestTimeout, readTimeout: defaultReadTimeout,
		writeTimeout: defaultWriteTimeout, idleTimeout: defaultIdleTimeout,
	}
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

func addObservabilityMiddlewares(b *registryAppConfig) error {
	if b.meterProvider != nil {
		metricsMiddleware, err := telemetry.MetricsMiddleware(b.meterProvider)
		if err != nil {
			return fmt.Errorf("failed to create metrics middleware: %w", err)
		}
		if metricsMiddleware != nil {
			b.middlewares = append([]func(http.Handler) http.Handler{metricsMiddleware}, b.middlewares...)
			slog.Info("HTTP metrics middleware enabled")
		}
	}
	if b.tracerProvider != nil {
		b.middlewares = append([]func(http.Handler) http.Handler{telemetry.TracingMiddleware(b.tracerProvider)}, b.middlewares...)
		slog.Info("HTTP tracing middleware enabled")
	}
	if b.config != nil && b.config.IsCompressionEnabled() {
		b.middlewares = append([]func(http.Handler) http.Handler{middleware.Compress(gzip.DefaultCompression)}, b.middlewares...)
		slog.Info("HTTP response compression enabled")
	}
	return nil
}

func buildAuditLogger(cfg *config.Config) (*auditmw.Logger, error) {
	if cfg == nil || !cfg.IsAuditEnabled() {
		return nil, nil
	}
	var logFile string
	if cfg.Audit != nil {
		logFile = cfg.Audit.LogFile
	}
	return auditmw.NewLogger(logFile)
}

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}
