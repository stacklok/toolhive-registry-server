package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestNewMeterProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		opts       []MeterProviderOption
		expectNoOp bool
	}{
		{
			name:       "returns no-op provider when no config provided",
			opts:       []MeterProviderOption{},
			expectNoOp: true,
		},
		{
			name: "returns no-op provider when metrics disabled",
			opts: []MeterProviderOption{
				WithMetricsConfig(&MetricsConfig{
					Enabled: false,
				}),
			},
			expectNoOp: true,
		},
		{
			name: "returns SDK provider when metrics enabled",
			opts: []MeterProviderOption{
				WithMetricsConfig(&MetricsConfig{
					Enabled: true,
				}),
				WithMeterInsecure(true),
			},
			expectNoOp: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			mp, err := NewMeterProvider(ctx, tt.opts...)

			require.NoError(t, err)
			require.NotNil(t, mp)

			if tt.expectNoOp {
				_, ok := mp.(noop.MeterProvider)
				assert.True(t, ok, "expected no-op meter provider")
			} else {
				sdkMP, ok := mp.(*sdkmetric.MeterProvider)
				assert.True(t, ok, "expected SDK meter provider")

				// Cleanup - ignore shutdown errors as there's no collector running
				// The OTLP exporter will try to flush metrics on shutdown, which fails
				// without a collector, but that's expected in tests
				if sdkMP != nil {
					_ = sdkMP.Shutdown(ctx)
				}
			}
		})
	}
}

func TestMeterProviderOptions(t *testing.T) {
	t.Parallel()

	t.Run("WithMeterServiceName sets service name", func(t *testing.T) {
		t.Parallel()
		cfg := &meterProviderConfig{}
		WithMeterServiceName("my-service")(cfg)
		assert.Equal(t, "my-service", cfg.serviceName)
	})

	t.Run("WithMeterServiceVersion sets service version", func(t *testing.T) {
		t.Parallel()
		cfg := &meterProviderConfig{}
		WithMeterServiceVersion("2.0.0")(cfg)
		assert.Equal(t, "2.0.0", cfg.serviceVersion)
	})

	t.Run("WithMetricsConfig sets metrics config", func(t *testing.T) {
		t.Parallel()
		metricsCfg := &MetricsConfig{Enabled: true}
		cfg := &meterProviderConfig{}
		WithMetricsConfig(metricsCfg)(cfg)
		assert.Equal(t, metricsCfg, cfg.metricsConfig)
	})

	t.Run("WithMeterEndpoint sets endpoint", func(t *testing.T) {
		t.Parallel()
		cfg := &meterProviderConfig{}
		WithMeterEndpoint("collector.example.com:4318")(cfg)
		assert.Equal(t, "collector.example.com:4318", cfg.endpoint)
	})

	t.Run("WithMeterInsecure sets insecure flag", func(t *testing.T) {
		t.Parallel()
		cfg := &meterProviderConfig{}
		WithMeterInsecure(true)(cfg)
		assert.True(t, cfg.insecure)
	})
}
