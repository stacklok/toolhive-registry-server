package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestNewRegistryMetrics(t *testing.T) {
	t.Parallel()

	t.Run("returns nil when provider is nil", func(t *testing.T) {
		t.Parallel()

		metrics, err := NewRegistryMetrics(nil)
		require.NoError(t, err)
		assert.Nil(t, metrics)
	})

	t.Run("creates metrics with SDK provider", func(t *testing.T) {
		t.Parallel()

		mp := sdkmetric.NewMeterProvider()
		defer func() { _ = mp.Shutdown(context.Background()) }()

		metrics, err := NewRegistryMetrics(mp)
		require.NoError(t, err)
		assert.NotNil(t, metrics)
		assert.NotNil(t, metrics.serversTotal)
	})
}

func TestRegistryMetrics_RecordServersTotal(t *testing.T) {
	t.Parallel()

	t.Run("no-op when metrics is nil", func(t *testing.T) {
		t.Parallel()

		var metrics *RegistryMetrics
		// Should not panic
		metrics.RecordServersTotal(context.Background(), "test-registry", 10)
	})

	t.Run("records server count with registry attribute", func(t *testing.T) {
		t.Parallel()

		reader := sdkmetric.NewManualReader()
		mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
		defer func() { _ = mp.Shutdown(context.Background()) }()

		metrics, err := NewRegistryMetrics(mp)
		require.NoError(t, err)
		require.NotNil(t, metrics)

		// Record some metrics
		metrics.RecordServersTotal(context.Background(), "prod-registry", 42)
		metrics.RecordServersTotal(context.Background(), "dev-registry", 10)

		// Collect metrics
		var rm metricdata.ResourceMetrics
		err = reader.Collect(context.Background(), &rm)
		require.NoError(t, err)

		// Verify metrics were recorded
		require.NotEmpty(t, rm.ScopeMetrics)

		// Find our registry metrics scope
		var foundScope bool
		for _, scope := range rm.ScopeMetrics {
			if scope.Scope.Name == RegistryMetricsMeterName {
				foundScope = true
				assert.NotEmpty(t, scope.Metrics)
			}
		}
		assert.True(t, foundScope, "expected to find registry metrics scope")
	})
}

func TestNewSyncMetrics(t *testing.T) {
	t.Parallel()

	t.Run("returns nil when provider is nil", func(t *testing.T) {
		t.Parallel()

		metrics, err := NewSyncMetrics(nil)
		require.NoError(t, err)
		assert.Nil(t, metrics)
	})

	t.Run("creates metrics with SDK provider", func(t *testing.T) {
		t.Parallel()

		mp := sdkmetric.NewMeterProvider()
		defer func() { _ = mp.Shutdown(context.Background()) }()

		metrics, err := NewSyncMetrics(mp)
		require.NoError(t, err)
		assert.NotNil(t, metrics)
		assert.NotNil(t, metrics.syncDuration)
	})
}

func TestSyncMetrics_RecordSyncDuration(t *testing.T) {
	t.Parallel()

	t.Run("no-op when metrics is nil", func(t *testing.T) {
		t.Parallel()

		var metrics *SyncMetrics
		// Should not panic
		metrics.RecordSyncDuration(context.Background(), "test-registry", 5*time.Second, true)
	})

	t.Run("records sync duration with attributes", func(t *testing.T) {
		t.Parallel()

		reader := sdkmetric.NewManualReader()
		mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
		defer func() { _ = mp.Shutdown(context.Background()) }()

		metrics, err := NewSyncMetrics(mp)
		require.NoError(t, err)
		require.NotNil(t, metrics)

		// Record successful sync
		metrics.RecordSyncDuration(context.Background(), "prod-registry", 2500*time.Millisecond, true)

		// Record failed sync
		metrics.RecordSyncDuration(context.Background(), "dev-registry", 500*time.Millisecond, false)

		// Collect metrics
		var rm metricdata.ResourceMetrics
		err = reader.Collect(context.Background(), &rm)
		require.NoError(t, err)

		// Verify metrics were recorded
		require.NotEmpty(t, rm.ScopeMetrics)

		// Find our sync metrics scope
		var foundScope bool
		for _, scope := range rm.ScopeMetrics {
			if scope.Scope.Name == SyncMetricsMeterName {
				foundScope = true
				assert.NotEmpty(t, scope.Metrics)

				// Verify we have the histogram metric
				for _, m := range scope.Metrics {
					if m.Name == "thv_reg_srv_sync_duration_seconds" {
						// Verify it's a histogram
						_, ok := m.Data.(metricdata.Histogram[float64])
						assert.True(t, ok, "expected histogram data type")
					}
				}
			}
		}
		assert.True(t, foundScope, "expected to find sync metrics scope")
	})

	t.Run("records duration in seconds", func(t *testing.T) {
		t.Parallel()

		reader := sdkmetric.NewManualReader()
		mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
		defer func() { _ = mp.Shutdown(context.Background()) }()

		metrics, err := NewSyncMetrics(mp)
		require.NoError(t, err)

		// Record a 1.5 second sync
		metrics.RecordSyncDuration(context.Background(), "test", 1500*time.Millisecond, true)

		// Collect and verify
		var rm metricdata.ResourceMetrics
		err = reader.Collect(context.Background(), &rm)
		require.NoError(t, err)

		// The histogram should have recorded 1.5 seconds
		for _, scope := range rm.ScopeMetrics {
			if scope.Scope.Name == SyncMetricsMeterName {
				for _, m := range scope.Metrics {
					if m.Name == "thv_reg_srv_sync_duration_seconds" {
						hist, ok := m.Data.(metricdata.Histogram[float64])
						require.True(t, ok)
						require.NotEmpty(t, hist.DataPoints)
						// Sum should be 1.5 (seconds)
						assert.InDelta(t, 1.5, hist.DataPoints[0].Sum, 0.001)
					}
				}
			}
		}
	})
}
