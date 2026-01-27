package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

func TestTracingMiddleware_NilProvider(t *testing.T) {
	t.Parallel()

	t.Run("returns pass-through middleware when provider is nil", func(t *testing.T) {
		t.Parallel()

		middleware := TracingMiddleware(nil)
		require.NotNil(t, middleware)

		// Create a simple handler that sets a flag when called
		handlerCalled := false
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			handlerCalled = true
			w.WriteHeader(http.StatusOK)
		})

		wrapped := middleware(handler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)

		assert.True(t, handlerCalled, "expected handler to be called")
		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("nil provider middleware does not modify response", func(t *testing.T) {
		t.Parallel()

		middleware := TracingMiddleware(nil)

		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Custom-Header", "test-value")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("response body"))
		})

		wrapped := middleware(handler)

		req := httptest.NewRequest(http.MethodPost, "/create", nil)
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusCreated, rr.Code)
		assert.Equal(t, "test-value", rr.Header().Get("X-Custom-Header"))
		assert.Equal(t, "response body", rr.Body.String())
	})
}

func TestTracingMiddleware_SpanCreation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		method         string
		path           string
		expectedMethod string
	}{
		{
			name:           "GET request creates span",
			method:         http.MethodGet,
			path:           "/users",
			expectedMethod: http.MethodGet,
		},
		{
			name:           "POST request creates span",
			method:         http.MethodPost,
			path:           "/users",
			expectedMethod: http.MethodPost,
		},
		{
			name:           "PUT request creates span",
			method:         http.MethodPut,
			path:           "/users/123",
			expectedMethod: http.MethodPut,
		},
		{
			name:           "DELETE request creates span",
			method:         http.MethodDelete,
			path:           "/users/123",
			expectedMethod: http.MethodDelete,
		},
		{
			name:           "PATCH request creates span",
			method:         http.MethodPatch,
			path:           "/users/123",
			expectedMethod: http.MethodPatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			exporter := tracetest.NewInMemoryExporter()
			tp := sdktrace.NewTracerProvider(
				sdktrace.WithSyncer(exporter),
			)
			defer func() { _ = tp.Shutdown(context.Background()) }()

			middleware := TracingMiddleware(tp)

			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			wrapped := middleware(handler)

			req := httptest.NewRequest(tt.method, tt.path, nil)
			rr := httptest.NewRecorder()
			wrapped.ServeHTTP(rr, req)

			spans := exporter.GetSpans()
			require.Len(t, spans, 1, "expected exactly one span")

			span := spans[0]
			// Verify the span has the correct HTTP method attribute
			var foundMethod bool
			for _, attr := range span.Attributes {
				if attr.Key == semconv.HTTPRequestMethodKey {
					assert.Equal(t, tt.expectedMethod, attr.Value.AsString())
					foundMethod = true
					break
				}
			}
			assert.True(t, foundMethod, "expected to find HTTP method attribute")
		})
	}
}

func TestTracingMiddleware_StatusCodeRecording(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		statusCode         int
		expectedSpanStatus codes.Code
		expectedStatusDesc string
	}{
		{
			name:               "200 OK sets span status to Ok",
			statusCode:         http.StatusOK,
			expectedSpanStatus: codes.Ok,
			expectedStatusDesc: "",
		},
		{
			name:               "201 Created sets span status to Ok",
			statusCode:         http.StatusCreated,
			expectedSpanStatus: codes.Ok,
			expectedStatusDesc: "",
		},
		{
			name:               "204 No Content sets span status to Ok",
			statusCode:         http.StatusNoContent,
			expectedSpanStatus: codes.Ok,
			expectedStatusDesc: "",
		},
		{
			name:               "301 Moved Permanently sets span status to Ok",
			statusCode:         http.StatusMovedPermanently,
			expectedSpanStatus: codes.Ok,
			expectedStatusDesc: "",
		},
		{
			name:               "400 Bad Request sets span status to Error",
			statusCode:         http.StatusBadRequest,
			expectedSpanStatus: codes.Error,
			expectedStatusDesc: http.StatusText(http.StatusBadRequest),
		},
		{
			name:               "401 Unauthorized sets span status to Error",
			statusCode:         http.StatusUnauthorized,
			expectedSpanStatus: codes.Error,
			expectedStatusDesc: http.StatusText(http.StatusUnauthorized),
		},
		{
			name:               "403 Forbidden sets span status to Error",
			statusCode:         http.StatusForbidden,
			expectedSpanStatus: codes.Error,
			expectedStatusDesc: http.StatusText(http.StatusForbidden),
		},
		{
			name:               "404 Not Found sets span status to Error",
			statusCode:         http.StatusNotFound,
			expectedSpanStatus: codes.Error,
			expectedStatusDesc: http.StatusText(http.StatusNotFound),
		},
		{
			name:               "500 Internal Server Error sets span status to Error",
			statusCode:         http.StatusInternalServerError,
			expectedSpanStatus: codes.Error,
			expectedStatusDesc: http.StatusText(http.StatusInternalServerError),
		},
		{
			name:               "502 Bad Gateway sets span status to Error",
			statusCode:         http.StatusBadGateway,
			expectedSpanStatus: codes.Error,
			expectedStatusDesc: http.StatusText(http.StatusBadGateway),
		},
		{
			name:               "503 Service Unavailable sets span status to Error",
			statusCode:         http.StatusServiceUnavailable,
			expectedSpanStatus: codes.Error,
			expectedStatusDesc: http.StatusText(http.StatusServiceUnavailable),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			exporter := tracetest.NewInMemoryExporter()
			tp := sdktrace.NewTracerProvider(
				sdktrace.WithSyncer(exporter),
			)
			defer func() { _ = tp.Shutdown(context.Background()) }()

			middleware := TracingMiddleware(tp)

			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
			})

			wrapped := middleware(handler)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rr := httptest.NewRecorder()
			wrapped.ServeHTTP(rr, req)

			spans := exporter.GetSpans()
			require.Len(t, spans, 1, "expected exactly one span")

			span := spans[0]
			assert.Equal(t, tt.expectedSpanStatus, span.Status.Code)
			assert.Equal(t, tt.expectedStatusDesc, span.Status.Description)

			// Verify status code attribute is recorded
			var foundStatusCode bool
			for _, attr := range span.Attributes {
				if attr.Key == semconv.HTTPResponseStatusCodeKey {
					assert.Equal(t, int64(tt.statusCode), attr.Value.AsInt64())
					foundStatusCode = true
					break
				}
			}
			assert.True(t, foundStatusCode, "expected to find HTTP response status code attribute")
		})
	}
}

func TestTracingMiddleware_TraceContextExtraction(t *testing.T) {
	t.Parallel()

	// Set up the global propagator for W3C Trace Context
	propagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	otel.SetTextMapPropagator(propagator)

	t.Run("extracts trace context from W3C traceparent header", func(t *testing.T) {
		t.Parallel()

		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(exporter),
		)
		defer func() { _ = tp.Shutdown(context.Background()) }()

		middleware := TracingMiddleware(tp)

		// Capture the span context inside the handler
		var capturedSpanCtx trace.SpanContext
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedSpanCtx = trace.SpanContextFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		})

		wrapped := middleware(handler)

		// Create request with W3C traceparent header
		// Format: version-traceId-parentId-flags
		// traceId: 32 hex chars, parentId: 16 hex chars
		expectedTraceID := "0af7651916cd43dd8448eb211c80319c"
		parentSpanID := "b7ad6b7169203331"
		traceparent := "00-" + expectedTraceID + "-" + parentSpanID + "-01"

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("traceparent", traceparent)
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)

		// Verify the captured span context has the extracted trace ID
		assert.Equal(t, expectedTraceID, capturedSpanCtx.TraceID().String())

		// Verify the recorded span also has the same trace ID
		spans := exporter.GetSpans()
		require.Len(t, spans, 1)
		assert.Equal(t, expectedTraceID, spans[0].SpanContext.TraceID().String())
	})

	t.Run("creates new trace context when no traceparent header present", func(t *testing.T) {
		t.Parallel()

		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(exporter),
		)
		defer func() { _ = tp.Shutdown(context.Background()) }()

		middleware := TracingMiddleware(tp)

		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		wrapped := middleware(handler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)

		spans := exporter.GetSpans()
		require.Len(t, spans, 1)

		// The span should have a valid (non-zero) trace ID
		assert.True(t, spans[0].SpanContext.TraceID().IsValid())
	})
}

func TestTracingMiddleware_RoutePatternExtraction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		routePattern    string
		requestPath     string
		expectedPattern string
	}{
		{
			name:            "extracts simple route pattern",
			routePattern:    "/servers",
			requestPath:     "/servers",
			expectedPattern: "/servers",
		},
		{
			name:            "extracts parameterized route pattern",
			routePattern:    "/servers/{name}",
			requestPath:     "/servers/my-server",
			expectedPattern: "/servers/{name}",
		},
		{
			name:            "extracts nested parameterized route pattern",
			routePattern:    "/users/{userID}/posts/{postID}",
			requestPath:     "/users/42/posts/123",
			expectedPattern: "/users/{userID}/posts/{postID}",
		},
		{
			name:            "extracts route with multiple segments",
			routePattern:    "/api/v1/resources/{id}/actions/{action}",
			requestPath:     "/api/v1/resources/abc-123/actions/delete",
			expectedPattern: "/api/v1/resources/{id}/actions/{action}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			exporter := tracetest.NewInMemoryExporter()
			tp := sdktrace.NewTracerProvider(
				sdktrace.WithSyncer(exporter),
			)
			defer func() { _ = tp.Shutdown(context.Background()) }()

			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			// Set up chi router with the tracing middleware
			r := chi.NewRouter()
			r.Use(TracingMiddleware(tp))
			r.Get(tt.routePattern, handler)

			req := httptest.NewRequest(http.MethodGet, tt.requestPath, nil)
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusOK, rr.Code)

			spans := exporter.GetSpans()
			require.Len(t, spans, 1)

			span := spans[0]

			// Verify span name uses route pattern
			expectedSpanName := "GET " + tt.expectedPattern
			assert.Equal(t, expectedSpanName, span.Name)

			// Verify HTTPRouteKey attribute uses route pattern
			var foundRouteKey bool
			for _, attr := range span.Attributes {
				if attr.Key == semconv.HTTPRouteKey {
					assert.Equal(t, tt.expectedPattern, attr.Value.AsString())
					foundRouteKey = true
					break
				}
			}
			assert.True(t, foundRouteKey, "expected to find HTTP route attribute")
		})
	}

	t.Run("uses unknown_route when route pattern is not available", func(t *testing.T) {
		t.Parallel()

		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(exporter),
		)
		defer func() { _ = tp.Shutdown(context.Background()) }()

		middleware := TracingMiddleware(tp)

		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		// Use middleware without chi router (no route context)
		wrapped := middleware(handler)

		req := httptest.NewRequest(http.MethodGet, "/some/path", nil)
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)

		spans := exporter.GetSpans()
		require.Len(t, spans, 1)

		span := spans[0]

		// Verify span name uses unknown_route
		expectedSpanName := "GET unknown_route"
		assert.Equal(t, expectedSpanName, span.Name)

		// Verify HTTPRouteKey attribute uses unknown_route
		var foundRouteKey bool
		for _, attr := range span.Attributes {
			if attr.Key == semconv.HTTPRouteKey {
				assert.Equal(t, "unknown_route", attr.Value.AsString())
				foundRouteKey = true
				break
			}
		}
		assert.True(t, foundRouteKey, "expected to find HTTP route attribute")
	})
}

func TestTracingMiddleware_SpanAttributes(t *testing.T) {
	t.Parallel()

	t.Run("records all expected span attributes", func(t *testing.T) {
		t.Parallel()

		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(exporter),
		)
		defer func() { _ = tp.Shutdown(context.Background()) }()

		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		r := chi.NewRouter()
		r.Use(TracingMiddleware(tp))
		r.Get("/test/path", handler)

		req := httptest.NewRequest(http.MethodGet, "/test/path", nil)
		req.Header.Set("User-Agent", "test-agent/1.0")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		spans := exporter.GetSpans()
		require.Len(t, spans, 1)

		span := spans[0]

		// Collect all attributes into a map for easier assertion
		attrs := make(map[string]interface{})
		for _, attr := range span.Attributes {
			switch attr.Value.Type().String() {
			case "STRING":
				attrs[string(attr.Key)] = attr.Value.AsString()
			case "INT64":
				attrs[string(attr.Key)] = attr.Value.AsInt64()
			default:
				attrs[string(attr.Key)] = attr.Value.AsInterface()
			}
		}

		// Verify HTTPRequestMethodKey
		assert.Equal(t, http.MethodGet, attrs[string(semconv.HTTPRequestMethodKey)])

		// Verify URLPath
		assert.Equal(t, "/test/path", attrs[string(semconv.URLPathKey)])

		// Verify UserAgentOriginal
		assert.Equal(t, "test-agent/1.0", attrs[string(semconv.UserAgentOriginalKey)])

		// Verify HTTPResponseStatusCode
		assert.Equal(t, int64(http.StatusOK), attrs[string(semconv.HTTPResponseStatusCodeKey)])

		// Verify HTTPRouteKey
		assert.Equal(t, "/test/path", attrs[string(semconv.HTTPRouteKey)])
	})

	t.Run("records empty user agent when not provided", func(t *testing.T) {
		t.Parallel()

		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(exporter),
		)
		defer func() { _ = tp.Shutdown(context.Background()) }()

		middleware := TracingMiddleware(tp)

		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		wrapped := middleware(handler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		// Explicitly clear User-Agent header
		req.Header.Del("User-Agent")
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)

		spans := exporter.GetSpans()
		require.Len(t, spans, 1)

		span := spans[0]

		var foundUserAgent bool
		for _, attr := range span.Attributes {
			if attr.Key == semconv.UserAgentOriginalKey {
				assert.Equal(t, "", attr.Value.AsString())
				foundUserAgent = true
				break
			}
		}
		assert.True(t, foundUserAgent, "expected to find user agent attribute even when empty")
	})
}

func TestTracingMiddleware_SpanKind(t *testing.T) {
	t.Parallel()

	t.Run("creates server span kind", func(t *testing.T) {
		t.Parallel()

		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(exporter),
		)
		defer func() { _ = tp.Shutdown(context.Background()) }()

		middleware := TracingMiddleware(tp)

		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		wrapped := middleware(handler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)

		spans := exporter.GetSpans()
		require.Len(t, spans, 1)

		assert.Equal(t, trace.SpanKindServer, spans[0].SpanKind)
	})
}

func TestTracingMiddleware_TracerName(t *testing.T) {
	t.Parallel()

	t.Run("uses correct tracer name", func(t *testing.T) {
		t.Parallel()

		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(exporter),
		)
		defer func() { _ = tp.Shutdown(context.Background()) }()

		middleware := TracingMiddleware(tp)

		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		wrapped := middleware(handler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)

		spans := exporter.GetSpans()
		require.Len(t, spans, 1)

		assert.Equal(t, TracerName, spans[0].InstrumentationScope.Name)
	})
}

func TestGetRoutePatternForTracing(t *testing.T) {
	t.Parallel()

	t.Run("returns unknown_route when no chi context", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/test/path", nil)
		pattern := getRoutePatternForTracing(req)

		assert.Equal(t, "unknown_route", pattern)
	})

	t.Run("returns unknown_route when chi context has empty pattern", func(t *testing.T) {
		t.Parallel()

		// Create a request with an empty chi route context
		req := httptest.NewRequest(http.MethodGet, "/test/path", nil)
		rctx := chi.NewRouteContext()
		ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
		req = req.WithContext(ctx)

		pattern := getRoutePatternForTracing(req)

		assert.Equal(t, "unknown_route", pattern)
	})

	t.Run("returns route pattern from chi context", func(t *testing.T) {
		t.Parallel()

		var capturedPattern string
		handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			capturedPattern = getRoutePatternForTracing(r)
		})

		r := chi.NewRouter()
		r.Get("/users/{id}", handler)

		req := httptest.NewRequest(http.MethodGet, "/users/123", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		assert.Equal(t, "/users/{id}", capturedPattern)
	})
}

func TestTracingMiddleware_ConcurrentRequests(t *testing.T) {
	t.Parallel()

	t.Run("handles concurrent requests correctly", func(t *testing.T) {
		t.Parallel()

		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(exporter),
		)
		defer func() { _ = tp.Shutdown(context.Background()) }()

		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		r := chi.NewRouter()
		r.Use(TracingMiddleware(tp))
		r.Get("/test", handler)

		// Make multiple concurrent requests
		const numRequests = 10
		done := make(chan bool, numRequests)

		for range numRequests {
			go func() {
				req := httptest.NewRequest(http.MethodGet, "/test", nil)
				rr := httptest.NewRecorder()
				r.ServeHTTP(rr, req)
				assert.Equal(t, http.StatusOK, rr.Code)
				done <- true
			}()
		}

		// Wait for all requests to complete
		for range numRequests {
			<-done
		}

		spans := exporter.GetSpans()
		assert.Len(t, spans, numRequests, "expected one span per request")

		// Verify all spans are valid
		for _, span := range spans {
			assert.True(t, span.SpanContext.TraceID().IsValid())
			assert.True(t, span.SpanContext.SpanID().IsValid())
		}
	})
}
