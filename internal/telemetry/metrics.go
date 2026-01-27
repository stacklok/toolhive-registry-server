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
	serversTotal metric.Int64Gauge
}

// NewRegistryMetrics creates a new RegistryMetrics instance with the given meter provider.
// If provider is nil, it returns nil (no-op metrics).
func NewRegistryMetrics(provider metric.MeterProvider) (*RegistryMetrics, error) {
	if provider == nil {
		return nil, nil
	}

	meter := provider.Meter(RegistryMetricsMeterName)

	serversTotal, err := meter.Int64Gauge(
		"thv_reg_srv_servers_total",
		metric.WithDescription("Number of servers in each registry"),
		metric.WithUnit("{server}"),
	)
	if err != nil {
		return nil, err
	}

	return &RegistryMetrics{
		serversTotal: serversTotal,
	}, nil
}

// RecordServersTotal records the current number of servers in a registry
func (m *RegistryMetrics) RecordServersTotal(ctx context.Context, registryName string, count int64) {
	if m == nil || m.serversTotal == nil {
		return
	}

	attrs := []attribute.KeyValue{
		attribute.String("registry", registryName),
	}

	m.serversTotal.Record(ctx, count, metric.WithAttributes(attrs...))
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
