package telemetry

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// findErrorsCounter returns the stacklok.registry.errors sum data, or a
// zero-value (no data points) if the metric wasn't recorded at all.
func findErrorsCounter(rm metricdata.ResourceMetrics) metricdata.Sum[int64] {
	for _, scope := range rm.ScopeMetrics {
		if scope.Scope.Name != HTTPMetricsMeterName {
			continue
		}
		for _, m := range scope.Metrics {
			if m.Name == "stacklok.registry.errors" {
				if sum, ok := m.Data.(metricdata.Sum[int64]); ok {
					return sum
				}
			}
		}
	}
	return metricdata.Sum[int64]{}
}

func TestNewHTTPMetrics(t *testing.T) {
	t.Parallel()

	t.Run("returns nil when provider is nil", func(t *testing.T) {
		t.Parallel()

		metrics, err := NewHTTPMetrics(nil)
		require.NoError(t, err)
		assert.Nil(t, metrics)
	})

	t.Run("creates metrics with SDK provider", func(t *testing.T) {
		t.Parallel()

		mp := sdkmetric.NewMeterProvider()
		defer func() { _ = mp.Shutdown(context.Background()) }()

		metrics, err := NewHTTPMetrics(mp)
		require.NoError(t, err)
		assert.NotNil(t, metrics)
		assert.NotNil(t, metrics.requestDuration)
		assert.NotNil(t, metrics.requestsTotal)
		assert.NotNil(t, metrics.activeRequests)
	})
}

func TestHTTPMetrics_Middleware(t *testing.T) {
	t.Parallel()

	t.Run("passes through when metrics is nil", func(t *testing.T) {
		t.Parallel()

		var metrics *HTTPMetrics
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		wrapped := metrics.Middleware(handler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("records metrics for successful request", func(t *testing.T) {
		t.Parallel()

		// Create a test meter provider with a reader to capture metrics
		reader := sdkmetric.NewManualReader()
		mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
		defer func() { _ = mp.Shutdown(context.Background()) }()

		metrics, err := NewHTTPMetrics(mp)
		require.NoError(t, err)
		require.NotNil(t, metrics)

		// Create a simple handler that returns 200
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		// Create chi router to test route pattern extraction
		r := chi.NewRouter()
		r.Use(metrics.Middleware)
		r.Get("/test/{id}", handler)

		// Make request
		req := httptest.NewRequest(http.MethodGet, "/test/123", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		// Collect metrics
		var rm metricdata.ResourceMetrics
		err = reader.Collect(context.Background(), &rm)
		require.NoError(t, err)

		// Verify metrics were recorded
		require.NotEmpty(t, rm.ScopeMetrics, "expected scope metrics to be recorded")

		// Find our HTTP metrics scope and assert the exact registered instrument
		// names, so a regression (e.g. reverting to thv_reg_srv_*) is caught here
		// rather than only passing because "some metric" was recorded.
		var foundScope bool
		var gotNames []string
		for _, scope := range rm.ScopeMetrics {
			if scope.Scope.Name == HTTPMetricsMeterName {
				foundScope = true
				for _, m := range scope.Metrics {
					gotNames = append(gotNames, m.Name)
				}
			}
		}
		assert.True(t, foundScope, "expected to find HTTP metrics scope")
		assert.Contains(t, gotNames, "http.server.request.duration")
		assert.Contains(t, gotNames, "stacklok.registry.http.requests")
		assert.Contains(t, gotNames, "http.server.active_requests")

		// Pin the semconv attribute keys/values on the duration histogram,
		// and confirm the pre-rename keys are gone: a revert to
		// attribute.String("route", ...) / strconv.Itoa(status) would
		// otherwise leave this suite green while every dashboard panel
		// grouping by (http_route) or (http_response_status_code) goes
		// empty in production.
		hist := findFloat64Histogram(t, rm, "http.server.request.duration")
		require.Len(t, hist.DataPoints, 1)
		dp := hist.DataPoints[0]

		expectedAttrs := attribute.NewSet(
			attribute.String("http.request.method", http.MethodGet),
			attribute.String("url.scheme", schemeHTTP),
			attribute.String("http.route", "/test/{id}"),
			attribute.Int("http.response.status_code", http.StatusOK),
		)
		assert.True(t, dp.Attributes.Equals(&expectedAttrs),
			"expected semconv HTTP attributes, got %v", dp.Attributes)

		for _, oldKey := range []attribute.Key{"method", "route", "status_code"} {
			_, present := dp.Attributes.Value(oldKey)
			assert.False(t, present, "pre-rename attribute key %q must not be emitted", oldKey)
		}
	})

	// semconv v1.26 marks http.request.method and url.scheme Required on
	// http.server.active_requests. Emitting it unlabeled would land as a single
	// anonymous series beside per-method series from every other
	// semconv-instrumented service in the same Prometheus.
	t.Run("records required semconv attributes on active_requests", func(t *testing.T) {
		t.Parallel()

		reader := sdkmetric.NewManualReader()
		mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
		defer func() { _ = mp.Shutdown(context.Background()) }()

		metrics, err := NewHTTPMetrics(mp)
		require.NoError(t, err)

		r := chi.NewRouter()
		r.Use(metrics.Middleware)
		r.Post("/things", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) })

		req := httptest.NewRequest(http.MethodPost, "/things", nil)
		r.ServeHTTP(httptest.NewRecorder(), req)

		var rm metricdata.ResourceMetrics
		require.NoError(t, reader.Collect(context.Background(), &rm))

		sum := findInt64Sum(t, rm, "http.server.active_requests")
		require.Len(t, sum.DataPoints, 1)

		expectedAttrs := attribute.NewSet(
			attribute.String("http.request.method", http.MethodPost),
			attribute.String("url.scheme", schemeHTTP),
		)
		assert.True(t, sum.DataPoints[0].Attributes.Equals(&expectedAttrs),
			"expected method and scheme on active_requests, got %v", sum.DataPoints[0].Attributes)

		// The +1 and -1 must carry identical attributes, or the series never
		// returns to zero and reads as a permanent in-flight leak.
		assert.Equal(t, int64(0), sum.DataPoints[0].Value,
			"active_requests must return to zero after the request completes")
	})

	t.Run("arbitrary request methods collapse to a single _OTHER series", func(t *testing.T) {
		t.Parallel()

		reader := sdkmetric.NewManualReader()
		mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
		defer func() { _ = mp.Shutdown(context.Background()) }()

		metrics, err := NewHTTPMetrics(mp)
		require.NoError(t, err)

		r := chi.NewRouter()
		r.Use(metrics.Middleware)
		r.Get("/things", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

		// net/http accepts any RFC 9110 token as a method, and this middleware
		// runs ahead of authentication, so these are reachable unauthenticated.
		// Without _OTHER normalization each one mints its own series on three
		// cumulative instruments, all exposed on the unauthenticated /metrics.
		const junkMethods = 25
		for i := range junkMethods {
			req := httptest.NewRequest(fmt.Sprintf("SCAN%04d", i), "/things", nil)
			r.ServeHTTP(httptest.NewRecorder(), req)
		}

		var rm metricdata.ResourceMetrics
		require.NoError(t, reader.Collect(context.Background(), &rm))

		hist := findFloat64Histogram(t, rm, "http.server.request.duration")
		assert.Len(t, hist.DataPoints, 1,
			"every unknown method must fold into one _OTHER series, got %d", len(hist.DataPoints))

		active := findInt64Sum(t, rm, "http.server.active_requests")
		assert.Len(t, active.DataPoints, 1,
			"active_requests had no attributes before url.scheme was added; it must not become unbounded now")

		method, present := hist.DataPoints[0].Attributes.Value(attribute.Key("http.request.method"))
		require.True(t, present)
		assert.Equal(t, "_OTHER", method.AsString())
	})

	t.Run("records error-by-type counter for 5xx and 4xx, but not 2xx", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name          string
			status        int
			expectPoint   bool
			expectedClass string
		}{
			{name: "500 counts as server_error", status: http.StatusInternalServerError, expectPoint: true, expectedClass: errorClassServer},
			{name: "404 counts as client_error", status: http.StatusNotFound, expectPoint: true, expectedClass: errorClassClient},
			{name: "200 records nothing", status: http.StatusOK, expectPoint: false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				reader := sdkmetric.NewManualReader()
				mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
				defer func() { _ = mp.Shutdown(context.Background()) }()

				metrics, err := NewHTTPMetrics(mp)
				require.NoError(t, err)

				handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(tt.status)
				})

				r := chi.NewRouter()
				r.Use(metrics.Middleware)
				r.Get("/error", handler)

				req := httptest.NewRequest(http.MethodGet, "/error", nil)
				rr := httptest.NewRecorder()
				r.ServeHTTP(rr, req)

				assert.Equal(t, tt.status, rr.Code)

				var rm metricdata.ResourceMetrics
				err = reader.Collect(context.Background(), &rm)
				require.NoError(t, err)

				hist := findErrorsCounter(rm)
				if !tt.expectPoint {
					assert.Empty(t, hist.DataPoints, "expected no stacklok.registry.errors data point for status %d", tt.status)
					return
				}
				require.Len(t, hist.DataPoints, 1)
				expectedAttrs := attribute.NewSet(
					attribute.String("error_type", tt.expectedClass),
					attribute.String("area", areaHTTP),
				)
				assert.True(t, hist.DataPoints[0].Attributes.Equals(&expectedAttrs),
					"expected error_type=%s, area=%s attributes, got %v", tt.expectedClass, areaHTTP, hist.DataPoints[0].Attributes)
			})
		}
	})

	t.Run("extracts route pattern from chi router", func(t *testing.T) {
		t.Parallel()

		reader := sdkmetric.NewManualReader()
		mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
		defer func() { _ = mp.Shutdown(context.Background()) }()

		metrics, err := NewHTTPMetrics(mp)
		require.NoError(t, err)

		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		r := chi.NewRouter()
		r.Use(metrics.Middleware)
		r.Get("/users/{userID}/posts/{postID}", handler)

		// Make request with specific IDs
		req := httptest.NewRequest(http.MethodGet, "/users/42/posts/123", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		// Verify the http.route attribute carries the route pattern, not the
		// actual request URL.
		var rm metricdata.ResourceMetrics
		err = reader.Collect(context.Background(), &rm)
		require.NoError(t, err)

		hist := findFloat64Histogram(t, rm, "http.server.request.duration")
		require.Len(t, hist.DataPoints, 1)

		route, present := hist.DataPoints[0].Attributes.Value(attribute.Key("http.route"))
		require.True(t, present, "expected http.route attribute to be set")
		assert.Equal(t, "/users/{userID}/posts/{postID}", route.AsString())
	})
}

func TestMetricsMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("returns no-op middleware when provider is nil", func(t *testing.T) {
		t.Parallel()

		mw, err := MetricsMiddleware(nil)
		require.NoError(t, err)
		require.NotNil(t, mw)

		// Test that the middleware passes through
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		wrapped := mw(handler)
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("returns working middleware with noop provider", func(t *testing.T) {
		t.Parallel()

		mp := noop.NewMeterProvider()
		mw, err := MetricsMiddleware(mp)
		require.NoError(t, err)
		require.NotNil(t, mw)

		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
		})

		wrapped := mw(handler)
		req := httptest.NewRequest(http.MethodPost, "/test", nil)
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusCreated, rr.Code)
	})

	t.Run("creates working middleware with SDK provider", func(t *testing.T) {
		t.Parallel()

		mp := sdkmetric.NewMeterProvider()
		defer func() { _ = mp.Shutdown(context.Background()) }()

		mw, err := MetricsMiddleware(mp)
		require.NoError(t, err)
		require.NotNil(t, mw)

		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		wrapped := mw(handler)
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
	})
}

func TestErrorClassForStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status   int
		expected string
	}{
		{status: http.StatusOK, expected: ""},
		{status: http.StatusFound, expected: ""},
		{status: http.StatusBadRequest, expected: errorClassClient},
		{status: http.StatusNotFound, expected: errorClassClient},
		{status: http.StatusInternalServerError, expected: errorClassServer},
		{status: http.StatusServiceUnavailable, expected: errorClassServer},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, errorClassForStatus(tt.status))
		})
	}
}

func TestRequestScheme(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		tls       bool
		forwarded string
		expected  string
	}{
		{name: "plaintext request", expected: schemeHTTP},
		{name: "TLS request", tls: true, expected: schemeHTTPS},
		{
			name:      "X-Forwarded-Proto wins over the plaintext proxy hop",
			forwarded: "https",
			expected:  schemeHTTPS,
		},
		{
			name:      "first entry wins through a proxy chain",
			forwarded: "https, http",
			expected:  schemeHTTPS,
		},
		{
			name:      "mixed case is normalised",
			forwarded: "HTTPS",
			expected:  schemeHTTPS,
		},
		{
			name:      "unrecognised value falls back rather than emitting it",
			forwarded: "gopher",
			expected:  schemeHTTP,
		},
		{
			name:      "unrecognised value falls back to the TLS hop",
			forwarded: "gopher",
			tls:       true,
			expected:  schemeHTTPS,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.tls {
				req.TLS = &tls.ConnectionState{}
			}
			if tt.forwarded != "" {
				req.Header.Set("X-Forwarded-Proto", tt.forwarded)
			}

			assert.Equal(t, tt.expected, requestScheme(req))
		})
	}
}

func TestGetRoutePattern(t *testing.T) {
	t.Parallel()

	t.Run("returns unknown_route when no chi context", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/test/path", nil)
		pattern := getRoutePattern(req)

		assert.Equal(t, "unknown_route", pattern)
	})

	t.Run("returns route pattern from chi context", func(t *testing.T) {
		t.Parallel()

		handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			pattern := getRoutePattern(r)
			assert.Equal(t, "/users/{id}", pattern)
		})

		r := chi.NewRouter()
		r.Get("/users/{id}", handler)

		req := httptest.NewRequest(http.MethodGet, "/users/123", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
	})
}

func TestHTTPMethodAttr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		method   string
		expected string
	}{
		{"known method passes through verbatim", http.MethodGet, http.MethodGet},
		{"every standard method is known", http.MethodDelete, http.MethodDelete},
		{"unknown token collapses to _OTHER", "SCAN0001", "_OTHER"},
		{"lowercase is not a known method", "get", "_OTHER"},
		{"empty method", "", "_OTHER"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			attr := httpMethodAttr(tt.method)
			assert.Equal(t, "http.request.method", string(attr.Key))
			assert.Equal(t, tt.expected, attr.Value.AsString())
		})
	}
}
