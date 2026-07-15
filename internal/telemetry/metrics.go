// Package telemetry provides OpenTelemetry instrumentation for the registry server.
package telemetry

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	// RegistryMetricsMeterName is the name used for the registry metrics meter
	RegistryMetricsMeterName = "github.com/stacklok/toolhive-registry-server/registry"

	// SyncMetricsMeterName is the name used for the sync metrics meter
	SyncMetricsMeterName = "github.com/stacklok/toolhive-registry-server/sync"
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
		"thv_reg_srv_servers_total",
		metric.WithDescription("Number of distinct servers in each source"),
		metric.WithUnit("{server}"),
	)
	if err != nil {
		return nil, err
	}

	skillsTotal, err := meter.Int64ObservableGauge(
		"thv_reg_srv_skills_total",
		metric.WithDescription("Number of distinct skills in each source"),
		metric.WithUnit("{skill}"),
	)
	if err != nil {
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
			serversTotal,
			skillsTotal,
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

// SyncMetrics holds the OpenTelemetry instruments for sync operation metrics
type SyncMetrics struct {
	syncDuration metric.Float64Histogram
}

// NewSyncMetrics creates a new SyncMetrics instance with the given meter provider.
// If provider is nil, it returns nil (no-op metrics).
func NewSyncMetrics(provider metric.MeterProvider) (*SyncMetrics, error) {
	if provider == nil {
		return nil, nil
	}

	meter := provider.Meter(SyncMetricsMeterName)

	syncDuration, err := meter.Float64Histogram(
		"thv_reg_srv_sync_duration_seconds",
		metric.WithDescription("Duration of sync operations in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300),
	)
	if err != nil {
		return nil, err
	}

	return &SyncMetrics{
		syncDuration: syncDuration,
	}, nil
}

// RecordSyncDuration records the duration of a sync operation for a registry
func (m *SyncMetrics) RecordSyncDuration(ctx context.Context, registryName string, duration time.Duration, success bool) {
	if m == nil || m.syncDuration == nil {
		return
	}

	attrs := []attribute.KeyValue{
		attribute.String("registry", registryName),
		attribute.Bool("success", success),
	}

	m.syncDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
}
