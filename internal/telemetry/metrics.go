// Package telemetry provides OpenTelemetry instrumentation for the registry server.
package telemetry

import (
	"context"
	"sync"
	"time"

	coremetrics "github.com/stacklok/toolhive-core/telemetry/metrics"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/stacklok/toolhive-registry-server/internal/versions"
)

const (
	// RegistryMetricsMeterName is the name used for the registry metrics meter
	RegistryMetricsMeterName = "github.com/stacklok/toolhive-registry-server/registry"

	// SyncMetricsMeterName is the name used for the sync metrics meter
	SyncMetricsMeterName = "github.com/stacklok/toolhive-registry-server/sync"

	// ComponentRegistry is this service's stacklok.component value (RFC D8).
	// toolhive-core defines only the AttrStacklokComponent key; each component
	// supplies its own value.
	ComponentRegistry = "registry"
)

// RegistryMetrics holds the OpenTelemetry instruments for registry metrics
type RegistryMetrics struct {
	serversTotal metric.Int64ObservableGauge
	skillsTotal  metric.Int64ObservableGauge
	pluginsTotal metric.Int64ObservableGauge
	registration metric.Registration
}

// RegistryMetricCount is the point-in-time count of registry entries for a source.
type RegistryMetricCount struct {
	SourceName  string
	ServerCount int64
	SkillCount  int64
	PluginCount int64
}

// RegistryMetricReader reads registry metric values at collection time.
type RegistryMetricReader interface {
	RegistryMetricCounts(ctx context.Context) ([]RegistryMetricCount, error)
}

// registryMetricCountsCacheTTL bounds how often the observable-gauge callback
// re-runs RegistryMetricCounts' underlying DB aggregate. Before the Prometheus
// reader was added, the periodic OTLP reader was the only caller, so the query
// ran once every DefaultMetricsInterval (60s); a Prometheus scrape interval
// shorter than that (commonly 15s, and unauthenticated on the internal port)
// would otherwise re-run the aggregate on every scrape. This TTL keeps the
// query rate bounded regardless of how many readers are attached or how often
// they're polled.
//
// It is deliberately strictly below DefaultMetricsInterval: at exactly 60s the
// periodic reader's every-60s tick would alternate hit/miss, halving the
// gauges' effective refresh rate to 120s in an OTLP-only deployment.
const registryMetricCountsCacheTTL = 30 * time.Second

// registryMetricCountsErrorCacheTTL is the negative TTL applied when the
// underlying read fails and there is no previous result to fall back on. It is
// much shorter than the success TTL: a failed collection costs the SDK *every*
// metric in the export (PeriodicReader.collectAndExport only exports when
// Collect returns nil, so sync, HTTP and build_info are dropped alongside the
// registry gauges), so a transient blip must not be held past its recovery.
const registryMetricCountsErrorCacheTTL = 5 * time.Second

// cachingRegistryMetricReader memoizes RegistryMetricCounts behind a TTL so
// concurrent or frequent callers (multiple readers, a fast scrape interval)
// share one query result instead of one query per caller. now is overridable
// in tests; it defaults to time.Now.
//
// A failed refresh serves the last known-good counts rather than propagating
// the error, because an error from an observable callback drops the entire
// export, not just these gauges. Slightly stale counts are far cheaper than a
// total telemetry gap. An error only reaches the caller when there has never
// been a successful read.
type cachingRegistryMetricReader struct {
	reader   RegistryMetricReader
	ttl      time.Duration
	errorTTL time.Duration
	now      func() time.Time

	mu        sync.Mutex
	expiresAt time.Time
	counts    []RegistryMetricCount
	haveCount bool
	err       error
}

func newCachingRegistryMetricReader(reader RegistryMetricReader) *cachingRegistryMetricReader {
	return &cachingRegistryMetricReader{
		reader:   reader,
		ttl:      registryMetricCountsCacheTTL,
		errorTTL: registryMetricCountsErrorCacheTTL,
		now:      time.Now,
	}
}

// RegistryMetricCounts returns the cached result if it's still within the TTL,
// otherwise refreshes it from the underlying reader.
func (c *cachingRegistryMetricReader) RegistryMetricCounts(ctx context.Context) ([]RegistryMetricCount, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Stamp the deadline from before the query, not after: measuring the TTL
	// from completion would push each expiry out by the query's own duration
	// and drift the refresh cadence past the export interval.
	started := c.now()
	if started.Before(c.expiresAt) {
		return c.counts, c.err
	}

	counts, err := c.reader.RegistryMetricCounts(ctx)
	if err != nil {
		// Fall back to the last known-good counts if we have any, so one
		// failed read doesn't black out the whole export.
		if c.haveCount {
			c.err = nil
			c.expiresAt = started.Add(c.errorTTL)
			return c.counts, nil
		}
		c.err = err
		c.expiresAt = started.Add(c.errorTTL)
		return nil, err
	}

	c.counts, c.haveCount, c.err = counts, true, nil
	c.expiresAt = started.Add(c.ttl)
	return c.counts, nil
}

// NewRegistryMetrics creates a new RegistryMetrics instance with the given meter provider.
// If provider is nil, it returns nil (no-op metrics).
func NewRegistryMetrics(provider metric.MeterProvider, reader RegistryMetricReader) (*RegistryMetrics, error) {
	if provider == nil {
		return nil, nil
	}

	meter := provider.Meter(RegistryMetricsMeterName)

	serversTotal, err := meter.Int64ObservableGauge(
		"stacklok.registry.servers",
		metric.WithDescription("Number of distinct servers in each source"),
		metric.WithUnit("{server}"),
	)
	if err != nil {
		return nil, err
	}

	skillsTotal, err := meter.Int64ObservableGauge(
		"stacklok.registry.skills",
		metric.WithDescription("Number of distinct skills in each source"),
		metric.WithUnit("{skill}"),
	)
	if err != nil {
		return nil, err
	}

	pluginsTotal, err := meter.Int64ObservableGauge(
		"stacklok.registry.plugins",
		metric.WithDescription("Number of distinct plugins in each source"),
		metric.WithUnit("{plugin}"),
	)
	if err != nil {
		return nil, err
	}

	info := versions.GetVersionInfo()
	if err := coremetrics.RegisterBuildInfo(meter, ComponentRegistry, info.Version, info.Commit); err != nil {
		return nil, err
	}

	var registration metric.Registration
	if reader != nil {
		cachedReader := newCachingRegistryMetricReader(reader)
		registration, err = meter.RegisterCallback(
			func(ctx context.Context, observer metric.Observer) error {
				counts, err := cachedReader.RegistryMetricCounts(ctx)
				if err != nil {
					return err
				}

				for _, count := range counts {
					attrs := metric.WithAttributes(attribute.String("source", count.SourceName))
					observer.ObserveInt64(serversTotal, count.ServerCount, attrs)
					observer.ObserveInt64(skillsTotal, count.SkillCount, attrs)
					observer.ObserveInt64(pluginsTotal, count.PluginCount, attrs)
				}

				return nil
			},
			serversTotal,
			skillsTotal,
			pluginsTotal,
		)
		if err != nil {
			return nil, err
		}
	}

	return &RegistryMetrics{
		serversTotal: serversTotal,
		skillsTotal:  skillsTotal,
		pluginsTotal: pluginsTotal,
		registration: registration,
	}, nil
}

// Unregister removes the observable callback registered for registry metrics.
// It does not unregister the stacklok.build_info gauge registered alongside
// it in NewRegistryMetrics: coremetrics.RegisterBuildInfo attaches its
// callback directly to the instrument via metric.WithInt64Callback and
// returns no metric.Registration, so there is nothing this method can
// capture or release. The build_info gauge keeps observing for the life of
// the meter provider even after Unregister is called.
func (m *RegistryMetrics) Unregister() error {
	if m == nil || m.registration == nil {
		return nil
	}

	return m.registration.Unregister()
}

// areaSync is the bounded area-label value stamped on
// stacklok.registry.errors for sync-path errors.
const areaSync = "sync"

// errorsCounterName, errorsCounterDescription, and errorsCounterUnit are
// shared by every meter (sync, HTTP) that registers the additive
// stacklok.registry.errors detail counter, so the instrument's identity
// can't drift between registration sites.
const (
	errorsCounterName        = "stacklok.registry.errors"
	errorsCounterDescription = "Errors by type and area (additive error-by-type detail counter)"
	errorsCounterUnit        = "{error}"
)

// newErrorsCounter registers the stacklok.registry.errors counter on the
// given meter. Shared by NewSyncMetrics and NewHTTPMetrics so both
// registrations stay identical.
func newErrorsCounter(meter metric.Meter) (metric.Int64Counter, error) {
	return meter.Int64Counter(
		errorsCounterName,
		metric.WithDescription(errorsCounterDescription),
		metric.WithUnit(errorsCounterUnit),
	)
}

// SyncMetrics holds the OpenTelemetry instruments for sync operation metrics
type SyncMetrics struct {
	syncDuration metric.Float64Histogram
	// errorsTotal is the additive error-by-type detail counter (RFC §3.6
	// coverage gap) for the sync path. error_type carries the structured
	// sync failure reason (a bounded condition-reason string), area is
	// the fixed "sync" value. Orthogonal to the outcome label on
	// syncDuration, which only distinguishes success from failure.
	errorsTotal metric.Int64Counter
}

// NewSyncMetrics creates a new SyncMetrics instance with the given meter provider.
// If provider is nil, it returns nil (no-op metrics).
func NewSyncMetrics(provider metric.MeterProvider) (*SyncMetrics, error) {
	if provider == nil {
		return nil, nil
	}

	meter := provider.Meter(SyncMetricsMeterName)

	syncDuration, err := meter.Float64Histogram(
		"stacklok.registry.sync.duration",
		metric.WithDescription("Duration of sync operations in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(coremetrics.BucketsLongRunning()...),
	)
	if err != nil {
		return nil, err
	}

	errorsTotal, err := newErrorsCounter(meter)
	if err != nil {
		return nil, err
	}

	return &SyncMetrics{
		syncDuration: syncDuration,
		errorsTotal:  errorsTotal,
	}, nil
}

// RecordSyncError increments stacklok.registry.errors for a sync failure,
// tagged with the bounded errorType (the structured sync condition reason) and
// the fixed area="sync" label. errorType is expected to be a bounded
// condition-reason string; an empty value falls back to "unknown" so the
// series never carries an empty label. No-op on a nil receiver / instrument.
func (m *SyncMetrics) RecordSyncError(ctx context.Context, errorType string) {
	if m == nil || m.errorsTotal == nil {
		return
	}
	if errorType == "" {
		errorType = "unknown"
	}
	m.errorsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String(coremetrics.LabelErrorType, errorType),
		attribute.String("area", areaSync),
	))
}

// RecordSyncDuration records the duration of a sync operation for a source.
// The outcome is emitted as the canonical string label "outcome" ("success" or
// "error"), never as a boolean. sourceName is the bounded "source" label — per
// the RFC's D6, "source"/"area" are concepts local to the registry component.
func (m *SyncMetrics) RecordSyncDuration(ctx context.Context, sourceName string, duration time.Duration, success bool) {
	if m == nil || m.syncDuration == nil {
		return
	}

	outcome := coremetrics.OutcomeSuccess
	if !success {
		outcome = coremetrics.OutcomeError
	}

	attrs := []attribute.KeyValue{
		attribute.String("source", sourceName),
		attribute.String(coremetrics.LabelOutcome, outcome),
	}

	m.syncDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
}
