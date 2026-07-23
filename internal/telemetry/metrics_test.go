package telemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type staticRegistryMetricReader struct {
	counts []RegistryMetricCount
	err    error
}

func (r *staticRegistryMetricReader) RegistryMetricCounts(_ context.Context) ([]RegistryMetricCount, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.counts, nil
}

func TestNewRegistryMetrics(t *testing.T) {
	t.Parallel()

	t.Run("returns nil when provider is nil", func(t *testing.T) {
		t.Parallel()

		metrics, err := NewRegistryMetrics(nil, nil)
		require.NoError(t, err)
		assert.Nil(t, metrics)
	})

	t.Run("creates metrics with SDK provider", func(t *testing.T) {
		t.Parallel()

		mp := sdkmetric.NewMeterProvider()
		defer func() { _ = mp.Shutdown(context.Background()) }()

		metrics, err := NewRegistryMetrics(mp, &staticRegistryMetricReader{})
		require.NoError(t, err)
		assert.NotNil(t, metrics)
		assert.NotNil(t, metrics.serversTotal)
	})
}

func TestRegistryMetrics_ObservableTotals(t *testing.T) {
	t.Parallel()

	t.Run("observes server and skill counts with source attribute", func(t *testing.T) {
		t.Parallel()

		reader := sdkmetric.NewManualReader()
		mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
		defer func() { _ = mp.Shutdown(context.Background()) }()

		metricsReader := &staticRegistryMetricReader{
			counts: []RegistryMetricCount{
				{SourceName: "prod-source", ServerCount: 42, SkillCount: 3},
				{SourceName: "dev-source", ServerCount: 10, SkillCount: 1},
			},
		}
		metrics, err := NewRegistryMetrics(mp, metricsReader)
		require.NoError(t, err)
		require.NotNil(t, metrics)

		var rm metricdata.ResourceMetrics
		err = reader.Collect(context.Background(), &rm)
		require.NoError(t, err)

		servers := findInt64Gauge(t, rm, "stacklok.registry.servers")
		require.Len(t, servers.DataPoints, 2)
		assertInt64GaugePoint(t, servers, "prod-source", 42)
		assertInt64GaugePoint(t, servers, "dev-source", 10)

		skills := findInt64Gauge(t, rm, "stacklok.registry.skills")
		require.Len(t, skills.DataPoints, 2)
		assertInt64GaugePoint(t, skills, "prod-source", 3)
		assertInt64GaugePoint(t, skills, "dev-source", 1)
	})

	t.Run("returns reader errors from collection", func(t *testing.T) {
		t.Parallel()

		reader := sdkmetric.NewManualReader()
		mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
		defer func() { _ = mp.Shutdown(context.Background()) }()

		expectedErr := errors.New("read failed")
		metrics, err := NewRegistryMetrics(mp, &staticRegistryMetricReader{err: expectedErr})
		require.NoError(t, err)
		require.NotNil(t, metrics)

		var rm metricdata.ResourceMetrics
		err = reader.Collect(context.Background(), &rm)
		require.ErrorIs(t, err, expectedErr)
	})
}

func findInt64Gauge(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Gauge[int64] {
	t.Helper()

	for _, scope := range rm.ScopeMetrics {
		if scope.Scope.Name != RegistryMetricsMeterName {
			continue
		}
		for _, m := range scope.Metrics {
			if m.Name == name {
				gauge, ok := m.Data.(metricdata.Gauge[int64])
				require.True(t, ok, "expected int64 gauge data type for %s", name)
				return gauge
			}
		}
	}

	require.FailNow(t, "metric not found", name)
	return metricdata.Gauge[int64]{}
}

func assertInt64GaugePoint(t *testing.T, gauge metricdata.Gauge[int64], sourceName string, value int64) {
	t.Helper()

	expectedAttrs := attribute.NewSet(attribute.String("source", sourceName))
	for _, point := range gauge.DataPoints {
		if point.Attributes.Equals(&expectedAttrs) {
			assert.Equal(t, value, point.Value)
			return
		}
	}

	require.FailNowf(t, "gauge point not found", "source=%s", sourceName)
}

func findFloat64Histogram(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Histogram[float64] {
	t.Helper()

	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == name {
				hist, ok := m.Data.(metricdata.Histogram[float64])
				require.True(t, ok, "expected float64 histogram data type for %s", name)
				return hist
			}
		}
	}

	require.FailNow(t, "metric not found", name)
	return metricdata.Histogram[float64]{}
}

func hasHistogramPoint(hist metricdata.Histogram[float64], attrs attribute.Set) bool {
	for _, dp := range hist.DataPoints {
		if dp.Attributes.Equals(&attrs) {
			return true
		}
	}
	return false
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

		hist := findFloat64Histogram(t, rm, "stacklok.registry.sync.duration")
		require.Len(t, hist.DataPoints, 2)

		// Outcome is the canonical string label, never a boolean.
		successAttrs := attribute.NewSet(
			attribute.String("registry", "prod-registry"),
			attribute.String("outcome", "success"),
		)
		errorAttrs := attribute.NewSet(
			attribute.String("registry", "dev-registry"),
			attribute.String("outcome", "error"),
		)
		assert.True(t, hasHistogramPoint(hist, successAttrs), "expected outcome=success data point for prod-registry")
		assert.True(t, hasHistogramPoint(hist, errorAttrs), "expected outcome=error data point for dev-registry")

		// A boolean success label must never be emitted.
		for _, dp := range hist.DataPoints {
			_, present := dp.Attributes.Value(attribute.Key("success"))
			assert.False(t, present, "boolean success label must not be emitted")
		}
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
					if m.Name == "stacklok.registry.sync.duration" {
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
