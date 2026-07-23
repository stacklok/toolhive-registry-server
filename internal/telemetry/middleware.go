// Package telemetry provides OpenTelemetry instrumentation for the registry server.
package telemetry

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	coremetrics "github.com/stacklok/toolhive-core/telemetry/metrics"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	// HTTPMetricsMeterName is the name used for the HTTP metrics meter
	HTTPMetricsMeterName = "github.com/stacklok/toolhive-registry-server/http"
)

// componentHTTP is the bounded component-label value stamped on
// stacklok.registry.errors for HTTP-layer errors.
const componentHTTP = "http"

// HTTPMetrics holds the OpenTelemetry instruments for HTTP metrics
type HTTPMetrics struct {
	requestDuration metric.Float64Histogram
	requestsTotal   metric.Int64Counter
	activeRequests  metric.Int64UpDownCounter
	// errorsTotal is the additive error-by-type detail counter (RFC §3.6
	// coverage gap). It carries the response status class as error_type
	// alongside the fixed component="http" label. It is orthogonal to the
	// status_code label already on requestsTotal: this series exists so an
	// error ratio can be split by class without a high-cardinality join.
	errorsTotal metric.Int64Counter
}

// NewHTTPMetrics creates a new HTTPMetrics instance with the given meter provider.
// If provider is nil, it returns nil (no-op metrics).
func NewHTTPMetrics(provider metric.MeterProvider) (*HTTPMetrics, error) {
	if provider == nil {
		return nil, nil
	}

	meter := provider.Meter(HTTPMetricsMeterName)

	requestDuration, err := meter.Float64Histogram(
		"stacklok.registry.http.request.duration",
		metric.WithDescription("Duration of HTTP requests in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(coremetrics.BucketsFastHTTP()...),
	)
	if err != nil {
		return nil, err
	}

	requestsTotal, err := meter.Int64Counter(
		"stacklok.registry.http.requests",
		metric.WithDescription("Total number of HTTP requests"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, err
	}

	activeRequests, err := meter.Int64UpDownCounter(
		"stacklok.registry.http.active_requests",
		metric.WithDescription("Number of currently in-flight HTTP requests"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, err
	}

	errorsTotal, err := meter.Int64Counter(
		"stacklok.registry.errors",
		metric.WithDescription("Errors by type and component (additive error-by-type detail counter)"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return nil, err
	}

	return &HTTPMetrics{
		requestDuration: requestDuration,
		requestsTotal:   requestsTotal,
		activeRequests:  activeRequests,
		errorsTotal:     errorsTotal,
	}, nil
}

// errorClassForStatus maps an HTTP status code to a bounded error_type value.
// Only 5xx and 4xx are classified as errors; anything below 400 returns "" and
// records nothing. Keeping the value to the status class (not the exact code)
// bounds cardinality on the error_type label.
func errorClassForStatus(status int) string {
	switch {
	case status >= 500:
		return "server_error"
	case status >= 400:
		return "client_error"
	default:
		return ""
	}
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

		// Additive error-by-type detail: increment on 5xx responses. The
		// status class (not the exact code) is the bounded error_type value;
		// area distinguishes this from sync/db errors on the same metric.
		if errType := errorClassForStatus(ww.Status()); errType == "server_error" {
			m.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String(coremetrics.LabelErrorType, errType),
				attribute.String("area", componentHTTP),
			))
		}
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
