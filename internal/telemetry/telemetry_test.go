package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// floatPtr is a helper function to create a pointer to a float64 value
func floatPtr(f float64) *float64 {
	return &f
}

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		opts             []Option
		expectNoOpTracer bool
		expectNoOpMeter  bool
		expectError      bool
		errorContains    string
	}{
		{
			name:             "returns no-op telemetry when no config provided",
			opts:             []Option{},
			expectNoOpTracer: true,
			expectNoOpMeter:  true,
		},
		{
			name: "returns no-op telemetry when disabled",
			opts: []Option{
				WithTelemetryConfig(&Config{
					Enabled: false,
				}),
			},
			expectNoOpTracer: true,
			expectNoOpMeter:  true,
		},
		{
			name: "returns no-op providers when both tracing and metrics disabled",
			opts: []Option{
				WithTelemetryConfig(&Config{
					Enabled: true,
					Tracing: &TracingConfig{
						Enabled: false,
					},
					Metrics: &MetricsConfig{
						Enabled: false,
					},
				}),
			},
			expectNoOpTracer: true,
			expectNoOpMeter:  true,
		},
		{
			name: "returns error for invalid sampling",
			opts: []Option{
				WithTelemetryConfig(&Config{
					Enabled: true,
					Tracing: &TracingConfig{
						Enabled:  true,
						Sampling: floatPtr(1.5),
					},
				}),
			},
			expectError:   true,
			errorContains: "invalid telemetry configuration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			tel, err := New(ctx, tt.opts...)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, tel)

			// Check tracer provider type
			if tt.expectNoOpTracer {
				_, ok := tel.TracerProvider().(tracenoop.TracerProvider)
				assert.True(t, ok, "expected no-op tracer provider")
			} else {
				_, ok := tel.TracerProvider().(*sdktrace.TracerProvider)
				assert.True(t, ok, "expected SDK tracer provider")
			}

			// Check meter provider type
			if tt.expectNoOpMeter {
				_, ok := tel.MeterProvider().(noop.MeterProvider)
				assert.True(t, ok, "expected no-op meter provider")
			} else {
				_, ok := tel.MeterProvider().(*sdkmetric.MeterProvider)
				assert.True(t, ok, "expected SDK meter provider")
			}

			// Cleanup
			require.NoError(t, tel.Shutdown(ctx))
		})
	}
}

func TestTelemetry_TracerProvider(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tel, err := New(ctx)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, tel.Shutdown(ctx))
	}()

	tp := tel.TracerProvider()
	require.NotNil(t, tp)
}

func TestTelemetry_MeterProvider(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tel, err := New(ctx)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, tel.Shutdown(ctx))
	}()

	mp := tel.MeterProvider()
	require.NotNil(t, mp)
}

func TestTelemetry_Tracer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tel, err := New(ctx)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, tel.Shutdown(ctx))
	}()

	tracer := tel.Tracer("test-tracer")
	require.NotNil(t, tracer)
}

func TestTelemetry_Meter(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tel, err := New(ctx)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, tel.Shutdown(ctx))
	}()

	meter := tel.Meter("test-meter")
	require.NotNil(t, meter)
}

func TestTelemetry_Shutdown(t *testing.T) {
	t.Parallel()

	t.Run("shutdown no-op telemetry succeeds", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		tel, err := New(ctx)
		require.NoError(t, err)

		err = tel.Shutdown(ctx)
		require.NoError(t, err)
	})

	t.Run("shutdown is idempotent for no-op telemetry", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		tel, err := New(ctx)
		require.NoError(t, err)

		// First shutdown
		err = tel.Shutdown(ctx)
		require.NoError(t, err)

		// Second shutdown should also succeed
		err = tel.Shutdown(ctx)
		require.NoError(t, err)
	})

	t.Run("shutdown SDK tracer provider succeeds", func(t *testing.T) {
		t.Parallel()

		// Create a mock OTLP server to accept trace exports
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()
		endpoint := strings.TrimPrefix(server.URL, "http://")

		ctx := context.Background()
		tel, err := New(ctx, WithTelemetryConfig(&Config{
			Enabled:  true,
			Endpoint: endpoint,
			Insecure: true,
			Tracing: &TracingConfig{
				Enabled:  true,
				Sampling: floatPtr(1.0),
			},
			Metrics: &MetricsConfig{
				Enabled: false,
			},
		}))
		require.NoError(t, err)

		// Verify we have an SDK tracer provider
		_, ok := tel.TracerProvider().(*sdktrace.TracerProvider)
		assert.True(t, ok, "expected SDK tracer provider")

		err = tel.Shutdown(ctx)
		require.NoError(t, err)
	})

	t.Run("shutdown SDK meter provider succeeds", func(t *testing.T) {
		t.Parallel()

		// Create a mock OTLP server to accept metric exports
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()
		endpoint := strings.TrimPrefix(server.URL, "http://")

		ctx := context.Background()
		tel, err := New(ctx, WithTelemetryConfig(&Config{
			Enabled:  true,
			Endpoint: endpoint,
			Insecure: true,
			Tracing: &TracingConfig{
				Enabled: false,
			},
			Metrics: &MetricsConfig{
				Enabled: true,
			},
		}))
		require.NoError(t, err)

		// Verify we have an SDK meter provider
		_, ok := tel.MeterProvider().(*sdkmetric.MeterProvider)
		assert.True(t, ok, "expected SDK meter provider")

		err = tel.Shutdown(ctx)
		require.NoError(t, err)
	})

	t.Run("shutdown both SDK providers succeeds", func(t *testing.T) {
		t.Parallel()

		// Create a mock OTLP server to accept trace and metric exports
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()
		endpoint := strings.TrimPrefix(server.URL, "http://")

		ctx := context.Background()
		tel, err := New(ctx, WithTelemetryConfig(&Config{
			Enabled:  true,
			Endpoint: endpoint,
			Insecure: true,
			Tracing: &TracingConfig{
				Enabled:  true,
				Sampling: floatPtr(1.0),
			},
			Metrics: &MetricsConfig{
				Enabled: true,
			},
		}))
		require.NoError(t, err)

		// Verify we have SDK providers
		_, okTracer := tel.TracerProvider().(*sdktrace.TracerProvider)
		assert.True(t, okTracer, "expected SDK tracer provider")
		_, okMeter := tel.MeterProvider().(*sdkmetric.MeterProvider)
		assert.True(t, okMeter, "expected SDK meter provider")

		err = tel.Shutdown(ctx)
		require.NoError(t, err)
	})
}

func TestOption_WithTelemetryConfig(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Enabled:     true,
		ServiceName: "test",
	}

	tc := &telemetryConfig{}
	WithTelemetryConfig(cfg)(tc)

	assert.Equal(t, cfg, tc.config)
}

func TestNewNoOpTelemetry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tel, err := newNoOpTelemetry(ctx)
	require.NoError(t, err)
	require.NotNil(t, tel)

	// Verify both providers are no-op
	_, okTracer := tel.TracerProvider().(tracenoop.TracerProvider)
	assert.True(t, okTracer, "expected no-op tracer provider")

	_, okMeter := tel.MeterProvider().(noop.MeterProvider)
	assert.True(t, okMeter, "expected no-op meter provider")

	// Cleanup
	require.NoError(t, tel.Shutdown(ctx))
}
