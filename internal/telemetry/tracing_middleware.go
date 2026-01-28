// Package telemetry provides OpenTelemetry instrumentation for the registry server.
package telemetry

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	// TracerName is the name used for the HTTP tracer
	TracerName = "github.com/stacklok/toolhive-registry-server/http"

	// MaxUserAgentLength is the maximum length for User-Agent strings in traces
	// to prevent unbounded storage from malicious or overly long User-Agent headers
	MaxUserAgentLength = 256
)

// lowValuePaths contains paths that should not be traced because they
// generate high-frequency, low-value spans (e.g., health checks, readiness probes).
var lowValuePaths = map[string]bool{
	"/health":    true,
	"/readiness": true,
}

// TracingMiddleware creates HTTP middleware for distributed tracing.
// If provider is nil, it returns a pass-through middleware that does nothing.
func TracingMiddleware(provider trace.TracerProvider) func(http.Handler) http.Handler {
	if provider == nil {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	tracer := provider.Tracer(TracerName)
	propagator := otel.GetTextMapPropagator()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip tracing for low-value endpoints (health checks, readiness probes)
			// These generate high-frequency spans with minimal diagnostic value
			if lowValuePaths[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			// Extract incoming trace context from request headers using W3C Trace Context propagation
			ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			// Wrap the response writer to capture the status code
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			// Get route pattern from chi context for the span name
			// Note: We get this after ServeHTTP to ensure chi has routed the request
			// For now, use the method and path; we'll update the span name after routing
			spanName := fmt.Sprintf("%s %s", r.Method, r.URL.Path)

			// Create the span with server kind and initial attributes
			ctx, span := tracer.Start(ctx, spanName,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					semconv.HTTPRequestMethodKey.String(r.Method),
					semconv.URLPath(r.URL.Path),
					semconv.UserAgentOriginal(truncateUserAgent(r.UserAgent())),
				),
			)
			defer span.End()

			// Serve the request with the new context containing the span
			next.ServeHTTP(ww, r.WithContext(ctx))

			// Get the route pattern from chi after routing has completed
			routePattern := getRoutePatternForTracing(r)

			// Update span name to use the route pattern instead of actual path
			// This prevents cardinality explosion from path parameters
			span.SetName(fmt.Sprintf("%s %s", r.Method, routePattern))

			// Add the route pattern as an attribute
			span.SetAttributes(semconv.HTTPRouteKey.String(routePattern))

			// Record the response status code
			statusCode := ww.Status()
			span.SetAttributes(semconv.HTTPResponseStatusCode(statusCode))

			// Set span status based on HTTP status code per OpenTelemetry semantic conventions.
			// Only 5xx server errors should set span status to Error.
			// 4xx client errors are expected responses and should leave status as Unset.
			// 2xx/3xx responses are successful and set status to Ok.
			if statusCode >= 500 {
				span.SetStatus(codes.Error, http.StatusText(statusCode))
			} else if statusCode < 400 {
				span.SetStatus(codes.Ok, "")
			}
			// 4xx errors: intentionally leave status as Unset (do not call SetStatus)
		})
	}
}

// getRoutePatternForTracing extracts the route pattern from a chi request context.
// Returns "unknown_route" if no pattern is found to prevent cardinality explosion.
func getRoutePatternForTracing(r *http.Request) string {
	rctx := chi.RouteContext(r.Context())
	if rctx != nil && rctx.RoutePattern() != "" {
		return rctx.RoutePattern()
	}
	// Return a constant to prevent cardinality explosion from unknown routes
	return "unknown_route"
}

// truncateUserAgent truncates a User-Agent string if it exceeds MaxUserAgentLength.
// This prevents unbounded storage in traces from malicious or overly long User-Agent headers.
func truncateUserAgent(ua string) string {
	if len(ua) <= MaxUserAgentLength {
		return ua
	}
	return ua[:MaxUserAgentLength]
}
