package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/stacklok/toolhive-registry-server/internal/otel"
	"github.com/stacklok/toolhive-registry-server/internal/telemetry"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// SyncTracerName is the tracer of sync spans, kept from v1.5.4's sync
// coordinator so existing trace queries keep working.
const SyncTracerName = "github.com/stacklok/toolhive-registry-server/sync/coordinator"

// syncSpanName is v1.5.4's span for one sync that ran.
const syncSpanName = "sync.performRegistrySync"

// syncErrorTypes maps failure categories to v1.5.4's error_type label values
// of stacklok_registry_errors_total{area="sync"}. Cancelled is new: v1.5.4
// did not count cancelled syncs as errors.
var syncErrorTypes = map[persistence.FailureCategory]string{
	persistence.FailureFetch:      "FetchFailed",
	persistence.FailureValidation: "ValidationFailed",
	persistence.FailureStorage:    "StorageFailed",
	persistence.FailureCancelled:  "Cancelled",
}

// observedJobs records metrics and a trace span for each sync that committed
// a snapshot or failed, as v1.5.4 did for each sync it ran. A poll that found
// the data unchanged (Complete) records neither. The wrapped Jobs stays
// authoritative; observation never changes results. metrics and tracer may
// each be nil.
type observedJobs struct {
	persistence.Jobs
	metrics *telemetry.SyncMetrics
	tracer  trace.Tracer
	mu      sync.Mutex
	// started holds the lease and start time of each source's current attempt.
	// It is keyed by source name, so an attempt that ends without Complete,
	// CommitSnapshot or Fail (a lost or stale lease) is replaced by the next
	// one instead of accumulating.
	started map[string]syncAttempt
}

type syncAttempt struct {
	lease string
	start time.Time
}

func newObservedJobs(jobs persistence.Jobs, metrics *telemetry.SyncMetrics, tracer trace.Tracer) *observedJobs {
	return &observedJobs{Jobs: jobs, metrics: metrics, tracer: tracer, started: map[string]syncAttempt{}}
}

func (j *observedJobs) ClaimNext(ctx context.Context, d time.Duration) (persistence.Job, bool, error) {
	job, ok, err := j.Jobs.ClaimNext(ctx, d)
	if err == nil && ok {
		j.start(job)
	}
	return job, ok, err
}

func (j *observedJobs) Acquire(ctx context.Context, n string, d time.Duration) (persistence.Job, error) {
	job, err := j.Jobs.Acquire(ctx, n, d)
	if err == nil {
		j.start(job)
	}
	return job, err
}

func (j *observedJobs) Complete(ctx context.Context, job persistence.Job, result persistence.SyncResult) error {
	err := j.Jobs.Complete(ctx, job, result)
	if err == nil {
		j.take(job) // unchanged data: v1.5.4 did not run (or record) a sync
	}
	return err
}

func (j *observedJobs) CommitSnapshot(
	ctx context.Context, job persistence.Job, result persistence.SyncResult, snapshot model.Snapshot,
) error {
	err := j.Jobs.CommitSnapshot(ctx, job, result, snapshot)
	if err == nil {
		if start, ok := j.take(job); ok {
			duration := time.Since(start)
			j.metrics.RecordSyncDuration(ctx, job.Source.Name, duration, true)
			j.span(ctx, job, start, duration, nil,
				attribute.Int("sync.server_count", len(snapshot.Data.Servers)),
				attribute.Int("sync.skill_count", len(snapshot.Data.Skills)),
				attribute.Int("sync.plugin_count", len(snapshot.Data.Plugins)),
			)
		}
	}
	return err
}

func (j *observedJobs) Fail(ctx context.Context, job persistence.Job, reason persistence.FailureCategory) error {
	err := j.Jobs.Fail(ctx, job, reason)
	if start, ok := j.take(job); ok {
		duration := time.Since(start)
		errorType, known := syncErrorTypes[reason]
		if !known {
			errorType = string(reason)
		}
		j.metrics.RecordSyncDuration(ctx, job.Source.Name, duration, false)
		j.metrics.RecordSyncError(ctx, errorType)
		j.span(ctx, job, start, duration, fmt.Errorf("sync failed: %s", reason))
	}
	return err
}

func (j *observedJobs) start(job persistence.Job) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.started[job.Source.Name] = syncAttempt{lease: job.LeaseID, start: time.Now()}
}

// take removes and returns the start of job's attempt, if it is the current one.
func (j *observedJobs) take(job persistence.Job) (time.Time, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	a, ok := j.started[job.Source.Name]
	if !ok || a.lease != job.LeaseID {
		return time.Time{}, false
	}
	delete(j.started, job.Source.Name)
	return a.start, true
}

// span records v1.5.4's sync span for a finished attempt, starting it at the
// attempt's start. failure is nil for a sync that committed.
func (j *observedJobs) span(
	ctx context.Context, job persistence.Job, start time.Time, duration time.Duration, failure error,
	attrs ...attribute.KeyValue,
) {
	if j.tracer == nil {
		return
	}
	_, span := j.tracer.Start(ctx, syncSpanName, trace.WithTimestamp(start), trace.WithAttributes(
		otel.AttrRegistryName.String(job.Source.Name),
		otel.AttrRegistryType.String(sourceType(job.Source)),
		attribute.Bool("sync.success", failure == nil),
		attribute.Float64("sync.duration_seconds", duration.Seconds()),
	))
	span.SetAttributes(attrs...)
	otel.RecordError(span, failure)
	span.End(trace.WithTimestamp(start.Add(duration)))
}

// sourceType names a source's type as v1.5.4's registry.type attribute did.
func sourceType(d persistence.SourceDefinition) string {
	switch {
	case d.Git != nil:
		return "git"
	case d.API != nil:
		return "api"
	case d.File != nil:
		return "file"
	case d.Managed != nil:
		return "managed"
	case d.Kubernetes != nil:
		return "kubernetes"
	default:
		return ""
	}
}
