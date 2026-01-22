package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// ptrFloat64 is a helper function to create a pointer to a float64 value
func ptrFloat64(f float64) *float64 {
	return &f
}

func TestNewTracerProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		opts       []TracerProviderOption
		expectNoOp bool
	}{
		{
			name:       "returns no-op provider when no config provided",
			opts:       []TracerProviderOption{},
			expectNoOp: true,
		},
		{
			name: "returns no-op provider when tracing disabled",
			opts: []TracerProviderOption{
				WithTracingConfig(&TracingConfig{
					Enabled: false,
				}),
			},
			expectNoOp: true,
		},
		{
			name: "returns SDK provider when tracing enabled",
			opts: []TracerProviderOption{
				WithTracingConfig(&TracingConfig{
					Enabled:  true,
					Sampling: ptrFloat64(0.5),
				}),
			},
			expectNoOp: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			tp, err := NewTracerProvider(ctx, tt.opts...)

			require.NoError(t, err)
			require.NotNil(t, tp)

			if tt.expectNoOp {
				_, ok := tp.(noop.TracerProvider)
				assert.True(t, ok, "expected no-op tracer provider")
			} else {
				_, ok := tp.(*sdktrace.TracerProvider)
				assert.True(t, ok, "expected SDK tracer provider")

				// Cleanup
				if sdkTP, ok := tp.(*sdktrace.TracerProvider); ok {
					require.NoError(t, sdkTP.Shutdown(ctx))
				}
			}
		})
	}
}

func TestTracerProviderOptions(t *testing.T) {
	t.Parallel()

	t.Run("WithTracerServiceName sets service name", func(t *testing.T) {
		t.Parallel()
		cfg := &tracerProviderConfig{}
		WithTracerServiceName("my-service")(cfg)
		assert.Equal(t, "my-service", cfg.serviceName)
	})

	t.Run("WithTracerServiceVersion sets service version", func(t *testing.T) {
		t.Parallel()
		cfg := &tracerProviderConfig{}
		WithTracerServiceVersion("2.0.0")(cfg)
		assert.Equal(t, "2.0.0", cfg.serviceVersion)
	})

	t.Run("WithTracingConfig sets tracing config", func(t *testing.T) {
		t.Parallel()
		tracingCfg := &TracingConfig{Enabled: true}
		cfg := &tracerProviderConfig{}
		WithTracingConfig(tracingCfg)(cfg)
		assert.Equal(t, tracingCfg, cfg.tracingConfig)
	})

	t.Run("WithTracerEndpoint sets endpoint", func(t *testing.T) {
		t.Parallel()
		cfg := &tracerProviderConfig{}
		WithTracerEndpoint("collector.example.com:4318")(cfg)
		assert.Equal(t, "collector.example.com:4318", cfg.endpoint)
	})

	t.Run("WithTracerInsecure sets insecure flag", func(t *testing.T) {
		t.Parallel()
		cfg := &tracerProviderConfig{}
		WithTracerInsecure(true)(cfg)
		assert.True(t, cfg.insecure)
	})
}
