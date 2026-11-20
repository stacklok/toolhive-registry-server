package standalone

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// Delays between attempts to build a worker that could not be built.
var (
	unbuiltRetryMin = time.Second
	unbuiltRetryMax = 5 * time.Minute
)

// unbuiltWorker stands in for a Kubernetes source whose worker could not be
// built, such as a stored definition with namespaces the operator rejects.
// The failure belongs to that source alone. Each distinct cause is logged and
// recorded on the source (which keeps its previous data, as after any failed
// sync), and the build is retried with backoff. Once a build succeeds the
// built worker runs in its place. A changed definition replaces this worker
// through the runtime's usual ownership check.
type unbuiltWorker struct {
	jobs       persistence.Jobs
	definition persistence.SourceDefinition
	build      func(persistence.SourceDefinition) (Worker, error)
	lease      time.Duration
	// err is the latest build failure; only Run reads or writes it.
	err error

	mu    sync.Mutex
	built Worker
}

// Ready is true while the build keeps failing: the failure is recorded and
// nothing is starting. After a build it reports the built worker's readiness.
func (w *unbuiltWorker) Ready() bool {
	w.mu.Lock()
	built := w.built
	w.mu.Unlock()
	return built == nil || built.Ready()
}

// Run records the build failure and retries the build until it succeeds or ctx
// ends. It returns only a storage-wide failure (recording the failure could
// not reach storage) or what the built worker returns.
func (w *unbuiltWorker) Run(ctx context.Context) error {
	delay := unbuiltRetryMin
	reported := ""
	for {
		if cause := ingest.RedactURLs(w.err.Error()); cause != reported {
			reported = cause
			slog.ErrorContext(ctx, "Failed to start source sync; other sources keep syncing",
				"source", w.definition.Name, "error", cause, "retry_in", delay.String())
			if err := w.record(ctx); err != nil {
				return err
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		worker, err := w.build(w.definition)
		if err == nil && worker != nil {
			slog.InfoContext(ctx, "Source sync started after earlier failures", "source", w.definition.Name)
			w.mu.Lock()
			w.built = worker
			w.mu.Unlock()
			return worker.Run(ctx)
		}
		if err == nil {
			err = persistence.ErrInvalid
		}
		w.err = err
		delay = min(2*delay, unbuiltRetryMax)
	}
}

// record leases the source and records the failure, so its status shows it.
// A lease held elsewhere (another replica) or a source that changed or was
// removed is skipped: its current owner reports its own state.
func (w *unbuiltWorker) record(ctx context.Context) error {
	category := persistence.FailureFetch
	if errors.Is(w.err, persistence.ErrInvalid) {
		category = persistence.FailureValidation
	}
	job, err := w.jobs.Acquire(ctx, w.definition.Name, w.lease)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, persistence.ErrBusy) || ownershipError(err) {
			slog.DebugContext(ctx, "Build failure not recorded on the source", "source", w.definition.Name, "error", err)
			return nil
		}
		return err
	}
	if job.Source.ID != w.definition.ID {
		// A newer incarnation: the runtime replaces this worker. The lease
		// expires on its own, as the coordinator leaves a stale lease.
		return nil
	}
	cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer done()
	if err := w.jobs.Fail(cleanup, job, category); err != nil && !ownershipError(err) {
		return err
	}
	return nil
}
