package telemetry

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
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

// countingRegistryMetricReader wraps another reader and counts how many
// times its RegistryMetricCounts was actually invoked, so cache-hit/miss
// behavior can be asserted directly.
type countingRegistryMetricReader struct {
	inner RegistryMetricReader
	calls int
}

func (r *countingRegistryMetricReader) RegistryMetricCounts(ctx context.Context) ([]RegistryMetricCount, error) {
	r.calls++
	return r.inner.RegistryMetricCounts(ctx)
}

// clockAdvancingFailingReader fails only after consuming dur of simulated
// wall-clock, modelling how this read actually fails in production: by
// exhausting registryMetricCountsQueryTimeout against a stalled or saturated
// database, rather than by an instant connection refusal.
type clockAdvancingFailingReader struct {
	clock *time.Time
	dur   time.Duration
	calls int
}

func (r *clockAdvancingFailingReader) RegistryMetricCounts(_ context.Context) ([]RegistryMetricCount, error) {
	r.calls++
	*r.clock = r.clock.Add(r.dur)
	return nil, errors.New("query timeout")
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

func TestCachingRegistryMetricReader(t *testing.T) {
	t.Parallel()

	t.Run("second call within TTL reuses the cached result", func(t *testing.T) {
		t.Parallel()

		inner := &countingRegistryMetricReader{
			inner: &staticRegistryMetricReader{counts: []RegistryMetricCount{{SourceName: "s", ServerCount: 1}}},
		}
		now := time.Now()
		cache := newCachingRegistryMetricReader(inner)
		cache.now = func() time.Time { return now }

		_, err := cache.RegistryMetricCounts(context.Background())
		require.NoError(t, err)
		_, err = cache.RegistryMetricCounts(context.Background())
		require.NoError(t, err)

		assert.Equal(t, 1, inner.calls, "expected the underlying reader to be queried once for two calls within the TTL")
	})

	t.Run("call after TTL expiry re-queries the underlying reader", func(t *testing.T) {
		t.Parallel()

		inner := &countingRegistryMetricReader{
			inner: &staticRegistryMetricReader{counts: []RegistryMetricCount{{SourceName: "s", ServerCount: 1}}},
		}
		now := time.Now()
		cache := newCachingRegistryMetricReader(inner)
		cache.now = func() time.Time { return now }

		_, err := cache.RegistryMetricCounts(context.Background())
		require.NoError(t, err)

		now = now.Add(RegistryMetricCountsCacheTTL + time.Second)
		_, err = cache.RegistryMetricCounts(context.Background())
		require.NoError(t, err)

		assert.Equal(t, 2, inner.calls, "expected a fresh query once the TTL has elapsed")
	})

	// The success TTL must stay strictly below the export interval. At exactly
	// DefaultMetricsInterval the periodic reader's tick would alternate
	// hit/miss and the gauges would only refresh every other cycle.
	t.Run("success TTL is below the metrics export interval", func(t *testing.T) {
		t.Parallel()

		assert.Less(t, RegistryMetricCountsCacheTTL, DefaultMetricsInterval,
			"a TTL at or above the export interval halves the gauges' effective refresh rate")
	})

	t.Run("TTL is measured from before the query, not after it", func(t *testing.T) {
		t.Parallel()

		now := time.Now()
		inner := &countingRegistryMetricReader{
			inner: &staticRegistryMetricReader{counts: []RegistryMetricCount{{SourceName: "s", ServerCount: 1}}},
		}
		cache := newCachingRegistryMetricReader(inner)
		cache.now = func() time.Time { return now }

		_, err := cache.RegistryMetricCounts(context.Background())
		require.NoError(t, err)

		assert.Equal(t, now.Add(RegistryMetricCountsCacheTTL), cache.expiresAt,
			"expiry stamped after the query would drift the refresh cadence by the query's duration")
	})

	t.Run("serves the last known-good counts when a refresh fails", func(t *testing.T) {
		t.Parallel()

		static := &staticRegistryMetricReader{counts: []RegistryMetricCount{{SourceName: "s", ServerCount: 7}}}
		inner := &countingRegistryMetricReader{inner: static}
		now := time.Now()
		cache := newCachingRegistryMetricReader(inner)
		cache.now = func() time.Time { return now }

		counts, err := cache.RegistryMetricCounts(context.Background())
		require.NoError(t, err)
		require.Len(t, counts, 1)

		// DB goes away; the next refresh fails.
		static.counts, static.err = nil, errors.New("db unavailable")
		now = now.Add(RegistryMetricCountsCacheTTL + time.Second)

		counts, err = cache.RegistryMetricCounts(context.Background())
		require.NoError(t, err,
			"an error from an observable callback drops the entire export, so stale counts are preferable")
		require.Len(t, counts, 1)
		assert.Equal(t, int64(7), counts[0].ServerCount)
	})

	t.Run("propagates the error when there has never been a successful read", func(t *testing.T) {
		t.Parallel()

		inner := &countingRegistryMetricReader{
			inner: &staticRegistryMetricReader{err: errors.New("db unavailable")},
		}
		now := time.Now()
		cache := newCachingRegistryMetricReader(inner)
		cache.now = func() time.Time { return now }

		_, err := cache.RegistryMetricCounts(context.Background())
		require.Error(t, err)
	})

	t.Run("caches a failure only for the short negative TTL", func(t *testing.T) {
		t.Parallel()

		static := &staticRegistryMetricReader{err: errors.New("db unavailable")}
		inner := &countingRegistryMetricReader{inner: static}
		now := time.Now()
		cache := newCachingRegistryMetricReader(inner)
		cache.now = func() time.Time { return now }

		_, err := cache.RegistryMetricCounts(context.Background())
		require.Error(t, err)

		// Within the negative TTL the failure is memoized, so a fast scrape
		// interval doesn't hammer an already-failing DB.
		_, err = cache.RegistryMetricCounts(context.Background())
		require.Error(t, err)
		assert.Equal(t, 1, inner.calls)

		// Once it lapses, a recovered DB is picked up immediately rather than
		// being held out for the full success TTL.
		static.err = nil
		static.counts = []RegistryMetricCount{{SourceName: "s", ServerCount: 1}}
		now = now.Add(RegistryMetricCountsErrorCacheTTL + time.Second)

		counts, err := cache.RegistryMetricCounts(context.Background())
		require.NoError(t, err)
		require.Len(t, counts, 1)
		assert.Equal(t, 2, inner.calls)
	})

	t.Run("negative TTL is shorter than the success TTL", func(t *testing.T) {
		t.Parallel()

		assert.Less(t, RegistryMetricCountsErrorCacheTTL, RegistryMetricCountsCacheTTL,
			"holding a failure for the full success TTL prolongs an export blackout past the DB's recovery")
	})

	// The sibling case above uses a reader that fails instantly, so it only
	// exercises the half of the negative TTL that holds regardless of how the
	// expiry is stamped. A read that fails by exhausting its query timeout
	// takes longer than the negative TTL itself, which is the case that
	// actually decides whether a struggling database gets left alone.
	t.Run("negative TTL holds when the read fails slowly, not just instantly", func(t *testing.T) {
		t.Parallel()

		clock := time.Now()
		inner := &clockAdvancingFailingReader{clock: &clock, dur: RegistryMetricCountsErrorCacheTTL * 4}
		cache := newCachingRegistryMetricReader(inner)
		cache.now = func() time.Time { return clock }

		_, err := cache.RegistryMetricCounts(context.Background())
		require.Error(t, err)

		// Immediately afterwards, with no idle time at all. Stamping the
		// negative TTL from before the query would have written an entry that
		// already expired while the query was running, letting this call
		// straight through to the database.
		_, err = cache.RegistryMetricCounts(context.Background())
		require.Error(t, err)

		assert.Equal(t, 1, inner.calls,
			"a slow failure must still be memoized: re-querying immediately turns an unreachable "+
				"database into a back-to-back stream of full-table aggregates")
	})

	t.Run("reports entering and leaving the degraded state", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		static := &staticRegistryMetricReader{counts: []RegistryMetricCount{{SourceName: "s", ServerCount: 1}}}
		now := time.Now()
		cache := newCachingRegistryMetricReader(static)
		cache.now = func() time.Time { return now }
		cache.logger = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

		_, err := cache.RegistryMetricCounts(context.Background())
		require.NoError(t, err)
		require.Empty(t, buf.String(), "a healthy refresh should say nothing")

		// Fail, so the reader starts serving stale counts. Without a log line
		// this is entirely invisible: the error is swallowed, and the gauges
		// keep reporting their last value with a fresh timestamp.
		static.err = errors.New("db unavailable")
		now = now.Add(RegistryMetricCountsCacheTTL + time.Second)
		counts, err := cache.RegistryMetricCounts(context.Background())
		require.NoError(t, err, "stale counts should still be served")
		require.Len(t, counts, 1)
		assert.Contains(t, buf.String(), "Registry metric counts refresh failed")
		assert.Contains(t, buf.String(), "serving_stale_counts=true")

		// Still failing: no second line, or a lengthy outage would emit one
		// per retry for as long as it lasts.
		buf.Reset()
		now = now.Add(RegistryMetricCountsErrorCacheTTL + time.Second)
		_, err = cache.RegistryMetricCounts(context.Background())
		require.NoError(t, err)
		assert.Empty(t, buf.String(), "the degraded state should be reported on transition, not per retry")

		static.err = nil
		now = now.Add(RegistryMetricCountsErrorCacheTTL + time.Second)
		_, err = cache.RegistryMetricCounts(context.Background())
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "Registry metric counts refresh recovered")
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
		metrics.RecordSyncDuration(context.Background(), "test-source", 5*time.Second, true)
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
		metrics.RecordSyncDuration(context.Background(), "prod-source", 2500*time.Millisecond, true)

		// Record failed sync
		metrics.RecordSyncDuration(context.Background(), "dev-source", 500*time.Millisecond, false)

		// Collect metrics
		var rm metricdata.ResourceMetrics
		err = reader.Collect(context.Background(), &rm)
		require.NoError(t, err)

		hist := findFloat64Histogram(t, rm, "stacklok.registry.sync.duration")
		require.Len(t, hist.DataPoints, 2)

		// Outcome is the canonical string label, never a boolean.
		successAttrs := attribute.NewSet(
			attribute.String("source", "prod-source"),
			attribute.String("outcome", "success"),
		)
		errorAttrs := attribute.NewSet(
			attribute.String("source", "dev-source"),
			attribute.String("outcome", "error"),
		)
		assert.True(t, hasHistogramPoint(hist, successAttrs), "expected outcome=success data point for prod-source")
		assert.True(t, hasHistogramPoint(hist, errorAttrs), "expected outcome=error data point for dev-source")

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

func findInt64Sum(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Sum[int64] {
	t.Helper()

	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == name {
				sum, ok := m.Data.(metricdata.Sum[int64])
				require.True(t, ok, "expected int64 sum data type for %s", name)
				return sum
			}
		}
	}

	require.FailNow(t, "metric not found", name)
	return metricdata.Sum[int64]{}
}

func TestSyncMetrics_RecordSyncError(t *testing.T) {
	t.Parallel()

	t.Run("no-op when metrics is nil", func(t *testing.T) {
		t.Parallel()

		var metrics *SyncMetrics
		// Should not panic
		metrics.RecordSyncError(context.Background(), "FetchFailed")
	})

	t.Run("records error with bounded error_type and fixed area=sync", func(t *testing.T) {
		t.Parallel()

		reader := sdkmetric.NewManualReader()
		mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
		defer func() { _ = mp.Shutdown(context.Background()) }()

		metrics, err := NewSyncMetrics(mp)
		require.NoError(t, err)
		require.NotNil(t, metrics)

		metrics.RecordSyncError(context.Background(), "FetchFailed")

		var rm metricdata.ResourceMetrics
		err = reader.Collect(context.Background(), &rm)
		require.NoError(t, err)

		sum := findInt64Sum(t, rm, "stacklok.registry.errors")
		require.Len(t, sum.DataPoints, 1)

		expectedAttrs := attribute.NewSet(
			attribute.String("error_type", "FetchFailed"),
			attribute.String("area", "sync"),
		)
		assert.True(t, sum.DataPoints[0].Attributes.Equals(&expectedAttrs))
	})

	t.Run("empty error type falls back to unknown", func(t *testing.T) {
		t.Parallel()

		reader := sdkmetric.NewManualReader()
		mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
		defer func() { _ = mp.Shutdown(context.Background()) }()

		metrics, err := NewSyncMetrics(mp)
		require.NoError(t, err)
		require.NotNil(t, metrics)

		metrics.RecordSyncError(context.Background(), "")

		var rm metricdata.ResourceMetrics
		err = reader.Collect(context.Background(), &rm)
		require.NoError(t, err)

		sum := findInt64Sum(t, rm, "stacklok.registry.errors")
		require.Len(t, sum.DataPoints, 1)

		expectedAttrs := attribute.NewSet(
			attribute.String("error_type", "unknown"),
			attribute.String("area", "sync"),
		)
		assert.True(t, sum.DataPoints[0].Attributes.Equals(&expectedAttrs))
	})
}
