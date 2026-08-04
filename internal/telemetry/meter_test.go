package telemetry

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	coremetrics "github.com/stacklok/toolhive-core/telemetry/metrics"
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

			mp, handler, err := NewMeterProvider(ctx, tt.opts...)

			require.NoError(t, err)
			require.NotNil(t, mp)

			if tt.expectNoOp {
				_, ok := mp.(noop.MeterProvider)
				assert.True(t, ok, "expected no-op meter provider")
				assert.Nil(t, handler, "no-op provider exposes no metrics handler")
			} else {
				sdkMP, ok := mp.(*sdkmetric.MeterProvider)
				assert.True(t, ok, "expected SDK meter provider")
				assert.NotNil(t, handler, "enabled provider exposes a Prometheus handler")

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

func TestNewMeterProvider_PrometheusHandlerScrape(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mp, handler, err := NewMeterProvider(ctx,
		WithMetricsConfig(&MetricsConfig{Enabled: true}),
		WithMeterInsecure(true),
	)
	require.NoError(t, err)
	require.NotNil(t, handler)

	sdkMP, ok := mp.(*sdkmetric.MeterProvider)
	require.True(t, ok, "expected SDK meter provider")
	t.Cleanup(func() { _ = sdkMP.Shutdown(ctx) })

	// Record a value on an instrument so the Prometheus registry has at
	// least one series to scrape, in addition to the always-present
	// stacklok.build_info-style target_info.
	meter := mp.Meter("meter_test")
	counter, err := meter.Int64Counter("test.scrape.counter")
	require.NoError(t, err)
	counter.Add(ctx, 1)

	require.NoError(t, coremetrics.RegisterBuildInfo(meter, ComponentRegistry, "1.2.3", "abcdef"))

	req := httptest.NewRequest("GET", "/metrics", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	require.Equal(t, 200, rr.Code)

	body, err := io.ReadAll(rr.Body)
	require.NoError(t, err)
	text := string(body)

	require.Contains(t, text, "test_scrape_counter_total", "recorded instrument should appear in the scrape")

	// stacklok.build_info is an Int64ObservableGauge with unit "1"; the OTel
	// Prometheus exporter appends "_ratio" to gauges with that unit
	// (otlptranslator's metric_namer.go), so the scraped name is
	// stacklok_build_info_ratio, not stacklok_build_info. Pin the actual
	// exported name so docs/observability.md can't silently drift from it.
	require.NotContains(t, text, "stacklok_build_info ", "build_info must not be exported without the _ratio suffix")
	require.Contains(t, text, "stacklok_build_info_ratio", "build_info gauge should be exported as stacklok_build_info_ratio")

	for line := range strings.SplitSeq(text, "\n") {
		if !strings.HasPrefix(line, "test_scrape_counter_total{") {
			continue
		}
		assert.Contains(t, line, `stacklok_component="registry"`,
			"every series must carry the D8 stacklok_component constant label")
		assert.Contains(t, line, "stacklok_product=", "every series must carry the D8 stacklok_product constant label")
		assert.NotContains(t, line, "host_name=", "host resource attributes must not leak into per-series labels")
		assert.NotContains(t, line, "process_pid=", "process resource attributes must not leak into per-series labels")
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
