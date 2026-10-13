package app

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/stacklok/toolhive-registry-server/internal/telemetry"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
)

type tracedCatalogFetcher struct {
	raw    reconcile.Fetcher
	tracer trace.Tracer
}

func (f tracedCatalogFetcher) Fetch(ctx context.Context, d persistence.SourceDefinition) (ingest.Result, error) {
	ctx, span := f.tracer.Start(ctx, "catalog.fetch", trace.WithAttributes(attribute.String("source", d.Name)))
	defer span.End()
	result, err := f.raw.Fetch(ctx, d)
	if err != nil {
		span.SetStatus(codes.Error, "source fetch failed")
	}
	return result, err
}

// catalogMetricReader gates and joins provider-owned scrapes too, including those
// whose exporters do not propagate the HTTP request context.
type catalogMetricReader struct {
	app *CatalogApp
	raw telemetry.RegistryMetricReader
}

func (r catalogMetricReader) RegistryMetricCounts(ctx context.Context) ([]telemetry.RegistryMetricCount, error) {
	ctx, done, err := r.app.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	return r.raw.RegistryMetricCounts(ctx)
}

// measuredCatalogJobs observes complete leased attempts, not merely fetch time.
// The underlying Jobs remains authoritative; metrics never affect its result.
type measuredCatalogJobs struct {
	persistence.Jobs
	metrics *telemetry.SyncMetrics
	mu      sync.Mutex
	started map[string]time.Time
}

func (j *measuredCatalogJobs) ClaimNext(ctx context.Context, d time.Duration) (persistence.Job, bool, error) {
	job, ok, err := j.Jobs.ClaimNext(ctx, d)
	if err == nil && ok {
		j.start(job)
	}
	return job, ok, err
}
func (j *measuredCatalogJobs) Acquire(ctx context.Context, n string, d time.Duration) (persistence.Job, error) {
	job, err := j.Jobs.Acquire(ctx, n, d)
	if err == nil {
		j.start(job)
	}
	return job, err
}
func (j *measuredCatalogJobs) Complete(ctx context.Context, job persistence.Job, result persistence.SyncResult) error {
	err := j.Jobs.Complete(ctx, job, result)
	if err == nil {
		j.finish(ctx, job, true, "")
	}
	return err
}
func (j *measuredCatalogJobs) CommitSnapshot(
	ctx context.Context, job persistence.Job, result persistence.SyncResult, snapshot model.Snapshot,
) error {
	err := j.Jobs.CommitSnapshot(ctx, job, result, snapshot)
	if err == nil {
		j.finish(ctx, job, true, "")
	}
	return err
}
func (j *measuredCatalogJobs) Fail(ctx context.Context, job persistence.Job, reason persistence.FailureCategory) error {
	err := j.Jobs.Fail(ctx, job, reason)
	j.finish(ctx, job, false, string(reason))
	return err
}
func (j *measuredCatalogJobs) start(job persistence.Job) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.started[job.LeaseID] = time.Now()
}
func (j *measuredCatalogJobs) finish(ctx context.Context, job persistence.Job, success bool, reason string) {
	j.mu.Lock()
	start, ok := j.started[job.LeaseID]
	delete(j.started, job.LeaseID)
	j.mu.Unlock()
	if ok {
		j.metrics.RecordSyncDuration(ctx, job.Source.Name, time.Since(start), success)
		if !success {
			j.metrics.RecordSyncError(ctx, reason)
		}
	}
}
