// Package telemetry provides OpenTelemetry instrumentation for the registry server.
package telemetry

import (
	"context"
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

	// DBMetricsMeterName is the name used for the database metrics meter
	DBMetricsMeterName = "github.com/stacklok/toolhive-registry-server/db"

	// ComponentRegistry is this service's stacklok.component value (RFC D8).
	// toolhive-core defines only the AttrStacklokComponent key; each component
	// supplies its own value.
	ComponentRegistry = "registry"
)

// RegistryMetrics holds the OpenTelemetry instruments for registry metrics
type RegistryMetrics struct {
	serversTotal metric.Int64ObservableGauge
	skillsTotal  metric.Int64ObservableGauge
	registration metric.Registration
}

// RegistryMetricCount is the point-in-time count of registry entries for a source.
type RegistryMetricCount struct {
	SourceName  string
	ServerCount int64
	SkillCount  int64
}

// RegistryMetricReader reads registry metric values at collection time.
type RegistryMetricReader interface {
	RegistryMetricCounts(ctx context.Context) ([]RegistryMetricCount, error)
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

	info := versions.GetVersionInfo()
	if err := coremetrics.RegisterBuildInfo(meter, ComponentRegistry, info.Version, info.Commit); err != nil {
		return nil, err
	}

	var registration metric.Registration
	if reader != nil {
		registration, err = meter.RegisterCallback(
			func(ctx context.Context, observer metric.Observer) error {
				counts, err := reader.RegistryMetricCounts(ctx)
				if err != nil {
					return err
				}

				for _, count := range counts {
					attrs := metric.WithAttributes(attribute.String("source", count.SourceName))
					observer.ObserveInt64(serversTotal, count.ServerCount, attrs)
					observer.ObserveInt64(skillsTotal, count.SkillCount, attrs)
				}

				return nil
			},
			serversTotal, skillsTotal,
		)
		if err != nil {
			return nil, err
		}
	}

	return &RegistryMetrics{
		serversTotal: serversTotal,
		skillsTotal:  skillsTotal,
		registration: registration,
	}, nil
}

// Unregister removes the observable callback registered for registry metrics.
func (m *RegistryMetrics) Unregister() error {
	if m == nil || m.registration == nil {
		return nil
	}

	return m.registration.Unregister()
}

// componentSync is the bounded component-label value stamped on
// stacklok.registry.errors for sync-path errors.
const componentSync = "sync"

// SyncMetrics holds the OpenTelemetry instruments for sync operation metrics
type SyncMetrics struct {
	syncDuration metric.Float64Histogram
	// errorsTotal is the additive error-by-type detail counter (RFC §3.6
	// coverage gap) for the sync path. error_type carries the structured
	// sync failure reason (a bounded condition-reason string), component is
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

	errorsTotal, err := meter.Int64Counter(
		"stacklok.registry.errors",
		metric.WithDescription("Errors by type and component (additive error-by-type detail counter)"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return nil, err
	}

	return &SyncMetrics{
		syncDuration: syncDuration,
		errorsTotal:  errorsTotal,
	}, nil
}

// DBMetrics holds the OpenTelemetry instruments for database query metrics.
type DBMetrics struct {
	queryDuration metric.Float64Histogram
}

// NewDBMetrics creates a new DBMetrics instance with the given meter provider.
// If provider is nil, it returns nil (no-op metrics).
func NewDBMetrics(provider metric.MeterProvider) (*DBMetrics, error) {
	if provider == nil {
		return nil, nil
	}

	meter := provider.Meter(DBMetricsMeterName)

	queryDuration, err := meter.Float64Histogram(
		"stacklok.registry.db.query.duration",
		metric.WithDescription("Duration of database queries in seconds, by operation"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(coremetrics.BucketsFastHTTP()...),
	)
	if err != nil {
		return nil, err
	}

	return &DBMetrics{queryDuration: queryDuration}, nil
}

// RecordQueryDuration records one database query duration observation labeled
// with the bounded operation name. operation must be a fixed query-name string
// (never raw SQL) to keep cardinality bounded. No-op on a nil receiver /
// instrument.
func (m *DBMetrics) RecordQueryDuration(ctx context.Context, operation string, duration time.Duration) {
	if m == nil || m.queryDuration == nil {
		return
	}
	m.queryDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(
		attribute.String("operation", operation),
	))
}

// RecordSyncError increments stacklok.registry.errors for a sync failure,
// tagged with the bounded errorType (the structured sync condition reason) and
// the fixed component="sync" label. errorType is expected to be a bounded
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
		attribute.String("area", componentSync),
	))
}

// RecordSyncDuration records the duration of a sync operation for a registry.
// The outcome is emitted as the canonical string label "outcome" ("success" or
// "error"), never as a boolean.
func (m *SyncMetrics) RecordSyncDuration(ctx context.Context, registryName string, duration time.Duration, success bool) {
	if m == nil || m.syncDuration == nil {
		return
	}

	outcome := coremetrics.OutcomeSuccess
	if !success {
		outcome = coremetrics.OutcomeError
	}

	attrs := []attribute.KeyValue{
		attribute.String("registry", registryName),
		attribute.String(coremetrics.LabelOutcome, outcome),
	}

	m.syncDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
}
