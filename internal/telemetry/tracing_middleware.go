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
)

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
					semconv.UserAgentOriginal(r.UserAgent()),
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

			// Set span status based on HTTP status code
			// 4xx errors are client errors, 5xx are server errors
			if statusCode >= 400 {
				span.SetStatus(codes.Error, http.StatusText(statusCode))
			} else {
				span.SetStatus(codes.Ok, "")
			}
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
