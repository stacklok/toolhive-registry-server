// Package telemetry provides OpenTelemetry instrumentation for the registry server.
package telemetry

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	// HTTPMetricsMeterName is the name used for the HTTP metrics meter
	HTTPMetricsMeterName = "github.com/stacklok/toolhive-registry-server/http"
)

// HTTPMetrics holds the OpenTelemetry instruments for HTTP metrics
type HTTPMetrics struct {
	requestDuration metric.Float64Histogram
	requestsTotal   metric.Int64Counter
	activeRequests  metric.Int64UpDownCounter
}

// NewHTTPMetrics creates a new HTTPMetrics instance with the given meter provider.
// If provider is nil, it returns nil (no-op metrics).
func NewHTTPMetrics(provider metric.MeterProvider) (*HTTPMetrics, error) {
	if provider == nil {
		return nil, nil
	}

	meter := provider.Meter(HTTPMetricsMeterName)

	requestDuration, err := meter.Float64Histogram(
		"thv_reg_srv_http_request_duration_seconds",
		metric.WithDescription("Duration of HTTP requests in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	if err != nil {
		return nil, err
	}

	requestsTotal, err := meter.Int64Counter(
		"thv_reg_srv_http_requests_total",
		metric.WithDescription("Total number of HTTP requests"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, err
	}

	activeRequests, err := meter.Int64UpDownCounter(
		"thv_reg_srv_http_active_requests",
		metric.WithDescription("Number of currently in-flight HTTP requests"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, err
	}

	return &HTTPMetrics{
		requestDuration: requestDuration,
		requestsTotal:   requestsTotal,
		activeRequests:  activeRequests,
	}, nil
}

// Middleware returns an HTTP middleware that records metrics for each request.
// If HTTPMetrics is nil, it returns a pass-through middleware.
func (m *HTTPMetrics) Middleware(next http.Handler) http.Handler {
	if m == nil {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture context at the start - it may be cancelled after ServeHTTP returns
		ctx := r.Context()
		start := time.Now()

		// Get route pattern from chi context (will be available after routing)
		// We need to wrap the response writer to capture the status code
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		// Increment active requests
		m.activeRequests.Add(ctx, 1)

		// Serve the request
		next.ServeHTTP(ww, r)

		// Decrement active requests after request completes
		m.activeRequests.Add(ctx, -1)

		// Get the route pattern from chi - this gives us the pattern like "/registry/v0.1/servers/{name}"
		// rather than the actual URL like "/registry/v0.1/servers/my-server"
		routePattern := getRoutePattern(r)

		// Record metrics
		attrs := []attribute.KeyValue{
			attribute.String("method", r.Method),
			attribute.String("route", routePattern),
			attribute.String("status_code", strconv.Itoa(ww.Status())),
		}

		duration := time.Since(start).Seconds()
		m.requestDuration.Record(ctx, duration, metric.WithAttributes(attrs...))
		m.requestsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
	})
}

// getRoutePattern extracts the route pattern from a chi request context.
// Returns "unknown_route" if no pattern is found to prevent cardinality explosion.
func getRoutePattern(r *http.Request) string {
	rctx := chi.RouteContext(r.Context())
	if rctx != nil && rctx.RoutePattern() != "" {
		return rctx.RoutePattern()
	}
	// Return a constant to prevent cardinality explosion from unknown routes
	return "unknown_route"
}

// MetricsMiddleware creates middleware from a MeterProvider for convenience.
// This is a helper function that combines NewHTTPMetrics and Middleware.
func MetricsMiddleware(provider metric.MeterProvider) (func(http.Handler) http.Handler, error) {
	metrics, err := NewHTTPMetrics(provider)
	if err != nil {
		return nil, err
	}

	// Return the middleware function
	return func(next http.Handler) http.Handler {
		if metrics == nil {
			return next
		}
		return metrics.Middleware(next)
	}, nil
}
