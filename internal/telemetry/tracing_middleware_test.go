package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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
)

// newTestTracerProvider creates a tracer provider with in-memory exporter for testing.
// The provider is automatically shut down when the test completes.
func newTestTracerProvider(t *testing.T) (*tracetest.InMemoryExporter, *sdktrace.TracerProvider) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return exporter, tp
}

func TestTracingMiddleware_NilProvider(t *testing.T) {
	t.Parallel()

	middleware := TracingMiddleware(nil)
	require.NotNil(t, middleware)

	// Verify pass-through behavior: handler called, response unmodified
	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handlerCalled = true
		w.Header().Set("X-Custom-Header", "test-value")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("response body"))
	})

	wrapped := middleware(handler)

	req := httptest.NewRequest(http.MethodPost, "/create", nil)
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	assert.True(t, handlerCalled, "expected handler to be called")
	assert.Equal(t, http.StatusCreated, rr.Code)
	assert.Equal(t, "test-value", rr.Header().Get("X-Custom-Header"))
	assert.Equal(t, "response body", rr.Body.String())
}

func TestTracingMiddleware_SpanCreation(t *testing.T) {
	t.Parallel()

	exporter, tp := newTestTracerProvider(t)
	middleware := TracingMiddleware(tp)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1, "expected exactly one span")

	// Verify the span has the correct HTTP method attribute
	var foundMethod bool
	for _, attr := range spans[0].Attributes {
		if attr.Key == semconv.HTTPRequestMethodKey {
			assert.Equal(t, http.MethodGet, attr.Value.AsString())
			foundMethod = true
			break
		}
	}
	assert.True(t, foundMethod, "expected to find HTTP method attribute")
}

func TestTracingMiddleware_StatusCodeRecording(t *testing.T) {
	t.Parallel()

	// Test only boundary cases: 2xx (Ok), 4xx (Unset), 5xx (Error)
	tests := []struct {
		name               string
		statusCode         int
		expectedSpanStatus codes.Code
		expectedStatusDesc string
	}{
		{
			name:               "2xx sets span status to Ok",
			statusCode:         http.StatusOK,
			expectedSpanStatus: codes.Ok,
			expectedStatusDesc: "",
		},
		{
			name:               "4xx leaves span status Unset",
			statusCode:         http.StatusNotFound,
			expectedSpanStatus: codes.Unset,
			expectedStatusDesc: "",
		},
		{
			name:               "5xx sets span status to Error",
			statusCode:         http.StatusInternalServerError,
			expectedSpanStatus: codes.Error,
			expectedStatusDesc: http.StatusText(http.StatusInternalServerError),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			exporter, tp := newTestTracerProvider(t)
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

	exporter, tp := newTestTracerProvider(t)
	middleware := TracingMiddleware(tp)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware(handler)

	// Create request with W3C traceparent header
	expectedTraceID := "0af7651916cd43dd8448eb211c80319c"
	parentSpanID := "b7ad6b7169203331"
	traceparent := "00-" + expectedTraceID + "-" + parentSpanID + "-01"

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("traceparent", traceparent)
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	// Verify the recorded span has the extracted trace ID
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, expectedTraceID, spans[0].SpanContext.TraceID().String())
}

func TestTracingMiddleware_RoutePatternExtraction(t *testing.T) {
	t.Parallel()

	t.Run("extracts parameterized route pattern", func(t *testing.T) {
		t.Parallel()

		exporter, tp := newTestTracerProvider(t)

		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		r := chi.NewRouter()
		r.Use(TracingMiddleware(tp))
		r.Get("/servers/{name}", handler)

		req := httptest.NewRequest(http.MethodGet, "/servers/my-server", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		spans := exporter.GetSpans()
		require.Len(t, spans, 1)

		// Verify span name uses route pattern (not actual path)
		assert.Equal(t, "GET /servers/{name}", spans[0].Name)

		// Verify HTTPRouteKey attribute uses route pattern
		var foundRouteKey bool
		for _, attr := range spans[0].Attributes {
			if attr.Key == semconv.HTTPRouteKey {
				assert.Equal(t, "/servers/{name}", attr.Value.AsString())
				foundRouteKey = true
				break
			}
		}
		assert.True(t, foundRouteKey, "expected to find HTTP route attribute")
	})

	t.Run("uses unknown_route when route pattern is not available", func(t *testing.T) {
		t.Parallel()

		exporter, tp := newTestTracerProvider(t)
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

		// Verify span name and attribute use unknown_route
		assert.Equal(t, "GET unknown_route", spans[0].Name)

		var foundRouteKey bool
		for _, attr := range spans[0].Attributes {
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

	exporter, tp := newTestTracerProvider(t)

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

	// Collect all attributes into a map for easier assertion
	attrs := make(map[string]interface{})
	for _, attr := range spans[0].Attributes {
		switch attr.Value.Type().String() {
		case "STRING":
			attrs[string(attr.Key)] = attr.Value.AsString()
		case "INT64":
			attrs[string(attr.Key)] = attr.Value.AsInt64()
		default:
			attrs[string(attr.Key)] = attr.Value.AsInterface()
		}
	}

	// Verify all expected attributes
	assert.Equal(t, http.MethodGet, attrs[string(semconv.HTTPRequestMethodKey)])
	assert.Equal(t, "/test/path", attrs[string(semconv.URLPathKey)])
	assert.Equal(t, "test-agent/1.0", attrs[string(semconv.UserAgentOriginalKey)])
	assert.Equal(t, int64(http.StatusOK), attrs[string(semconv.HTTPResponseStatusCodeKey)])
	assert.Equal(t, "/test/path", attrs[string(semconv.HTTPRouteKey)])
}

func TestTracingMiddleware_SkipsLowValueEndpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
	}{
		{name: "health endpoint", path: "/health"},
		{name: "readiness endpoint", path: "/readiness"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			exporter, tp := newTestTracerProvider(t)
			middleware := TracingMiddleware(tp)

			handlerCalled := false
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				handlerCalled = true
				w.WriteHeader(http.StatusOK)
			})

			wrapped := middleware(handler)

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rr := httptest.NewRecorder()
			wrapped.ServeHTTP(rr, req)

			// Handler should still be called
			assert.True(t, handlerCalled, "handler should be called")
			assert.Equal(t, http.StatusOK, rr.Code)

			// But no spans should be created for low-value endpoints
			spans := exporter.GetSpans()
			assert.Empty(t, spans, "should not create spans for %s", tt.path)
		})
	}
}

func TestTruncateUserAgent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "short user agent unchanged",
			input:    "Mozilla/5.0",
			expected: "Mozilla/5.0",
		},
		{
			name:     "exactly max length unchanged",
			input:    strings.Repeat("a", MaxUserAgentLength),
			expected: strings.Repeat("a", MaxUserAgentLength),
		},
		{
			name:     "exceeds max length truncated",
			input:    strings.Repeat("a", MaxUserAgentLength+100),
			expected: strings.Repeat("a", MaxUserAgentLength),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := truncateUserAgent(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
