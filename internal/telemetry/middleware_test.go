package telemetry

import (
	"context"
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

		// Find our HTTP metrics scope
		var foundScope bool
		for _, scope := range rm.ScopeMetrics {
			if scope.Scope.Name == HTTPMetricsMeterName {
				foundScope = true
				assert.NotEmpty(t, scope.Metrics, "expected metrics to be recorded")
			}
		}
		assert.True(t, foundScope, "expected to find HTTP metrics scope")
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

		// Verify metrics were recorded (route pattern should be used, not actual URL)
		var rm metricdata.ResourceMetrics
		err = reader.Collect(context.Background(), &rm)
		require.NoError(t, err)
		require.NotEmpty(t, rm.ScopeMetrics)
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
