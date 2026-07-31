// Package telemetry provides OpenTelemetry instrumentation for the registry server.
package telemetry

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	coremetrics "github.com/stacklok/toolhive-core/telemetry/metrics"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

const (
	// HTTPMetricsMeterName is the name used for the HTTP metrics meter
	HTTPMetricsMeterName = "github.com/stacklok/toolhive-registry-server/http"
)

// areaHTTP is the bounded area-label value stamped on
// stacklok.registry.errors for HTTP-layer errors.
const areaHTTP = "http"

// schemeHTTP and schemeHTTPS are the only url.scheme values this server emits.
const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

// HTTPMetrics holds the OpenTelemetry instruments for HTTP metrics
type HTTPMetrics struct {
	requestDuration metric.Float64Histogram
	requestsTotal   metric.Int64Counter
	activeRequests  metric.Int64UpDownCounter
	// errorsTotal is the additive error-by-type detail counter (RFC §3.6
	// coverage gap). It carries the response status class as error_type
	// alongside the fixed area="http" label. It is orthogonal to the
	// http.response.status_code attribute already on requestsTotal: this
	// series exists so an error ratio can be split by class without a
	// high-cardinality join.
	errorsTotal metric.Int64Counter
}

// NewHTTPMetrics creates a new HTTPMetrics instance with the given meter provider.
// If provider is nil, it returns nil (no-op metrics).
func NewHTTPMetrics(provider metric.MeterProvider) (*HTTPMetrics, error) {
	if provider == nil {
		return nil, nil
	}

	meter := provider.Meter(HTTPMetricsMeterName)

	// requestDuration and activeRequests carry the OTel HTTP server semconv
	// names and units verbatim (RFC D2: semconv-covered metrics are never
	// prefixed), so they stay cross-vendor comparable.
	requestDuration, err := meter.Float64Histogram(
		semconv.HTTPServerRequestDurationName,
		metric.WithDescription(semconv.HTTPServerRequestDurationDescription),
		metric.WithUnit(semconv.HTTPServerRequestDurationUnit),
		metric.WithExplicitBucketBoundaries(coremetrics.BucketsFastHTTP()...),
	)
	if err != nil {
		return nil, err
	}

	// requestsTotal has no semconv equivalent (semconv derives rate from the
	// duration histogram's count), so it keeps the stacklok.* prefix.
	requestsTotal, err := meter.Int64Counter(
		"stacklok.registry.http.requests",
		metric.WithDescription("Total number of HTTP requests"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, err
	}

	activeRequests, err := meter.Int64UpDownCounter(
		semconv.HTTPServerActiveRequestsName,
		metric.WithDescription(semconv.HTTPServerActiveRequestsDescription),
		metric.WithUnit(semconv.HTTPServerActiveRequestsUnit),
	)
	if err != nil {
		return nil, err
	}

	errorsTotal, err := newErrorsCounter(meter)
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

// errorClassServer and errorClassClient are the bounded error_type values
// errorClassForStatus can return.
const (
	errorClassServer = "server_error"
	errorClassClient = "client_error"
)

// errorClassForStatus maps an HTTP status code to a bounded error_type value.
// Only 5xx and 4xx are classified as errors; anything below 400 returns "" and
// records nothing. Keeping the value to the status class (not the exact code)
// bounds cardinality on the error_type label.
func errorClassForStatus(status int) string {
	switch {
	case status >= 500:
		return errorClassServer
	case status >= 400:
		return errorClassClient
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

		// semconv v1.26 marks http.request.method and url.scheme Required on
		// http.server.active_requests. Both are resolved before serving and
		// reused on the decrement, so every series returns to zero — an
		// unlabeled +1 paired with a labeled -1 would leak in-flight counts.
		activeAttrs := metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String(r.Method),
			semconv.URLScheme(requestScheme(r)),
		)

		// Increment active requests
		m.activeRequests.Add(ctx, 1, activeAttrs)

		// Serve the request
		next.ServeHTTP(ww, r)

		// Decrement active requests after request completes
		m.activeRequests.Add(ctx, -1, activeAttrs)

		// Get the route pattern from chi - this gives us the pattern like "/registry/v0.1/servers/{name}"
		// rather than the actual URL like "/registry/v0.1/servers/my-server"
		routePattern := getRoutePattern(r)

		// Record metrics using semconv HTTP attribute keys, so a
		// http.server.request.duration series stays joinable with the same
		// metric emitted by any other semconv-instrumented service. url.scheme
		// is Required on that metric alongside http.request.method.
		attrs := []attribute.KeyValue{
			semconv.HTTPRequestMethodKey.String(r.Method),
			semconv.URLScheme(requestScheme(r)),
			semconv.HTTPRoute(routePattern),
			semconv.HTTPResponseStatusCode(ww.Status()),
		}

		duration := time.Since(start).Seconds()
		m.requestDuration.Record(ctx, duration, metric.WithAttributes(attrs...))
		m.requestsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))

		// Additive error-by-type detail: increment on 4xx and 5xx responses.
		// The status class (not the exact code) is the bounded error_type
		// value; area distinguishes this from sync/db errors on the same
		// metric.
		if errType := errorClassForStatus(ww.Status()); errType != "" {
			m.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String(coremetrics.LabelErrorType, errType),
				attribute.String("area", areaHTTP),
			))
		}
	})
}

// requestScheme returns the url.scheme value for a request. semconv v1.26
// marks url.scheme Required on both http.server.request.duration and
// http.server.active_requests, and the value is bounded to http/https.
//
// r.URL.Scheme is empty on server-side requests, and r.TLS reflects the hop
// into this process — which is plaintext when a TLS-terminating proxy sits in
// front — so X-Forwarded-Proto takes precedence when present.
func requestScheme(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		// A comma-separated list means multiple proxies; the first entry is
		// the original client-facing scheme.
		if comma := strings.IndexByte(forwarded, ','); comma >= 0 {
			forwarded = forwarded[:comma]
		}
		switch scheme := strings.ToLower(strings.TrimSpace(forwarded)); scheme {
		case schemeHTTP, schemeHTTPS:
			return scheme
		}
	}

	if r.TLS != nil {
		return schemeHTTPS
	}
	return schemeHTTP
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
