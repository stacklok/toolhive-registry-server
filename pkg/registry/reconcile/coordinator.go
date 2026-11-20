// Package reconcile coordinates claims-free source ingestion over persistence.Jobs.
// It never owns a database, listener, identity policy or supplied client.
package reconcile

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/gobwas/glob"

	"github.com/stacklok/toolhive-registry-server/internal/filtering"
	"github.com/stacklok/toolhive-registry-server/internal/shutdown"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

//go:generate mockgen -destination=mocks/fetcher.go -package=mocks . Fetcher,ChangeFetcher
//go:generate mockgen -destination=mocks/jobs.go -package=mocks github.com/stacklok/toolhive-registry-server/pkg/registry/persistence Jobs

// Fetcher must return a complete snapshot, never partial data on failure. It must
// honor cancellation and must not mutate the captured definition. It applies its
// source's acceptance policy; the coordinator checks storage safety after
// filtering. A failure's detail is attached with ingest.WithCause: the
// coordinator logs it and returns only the category.
type Fetcher interface {
	Fetch(context.Context, persistence.SourceDefinition) (ingest.Result, error)
}

// ChangeFetcher is an optional Fetcher extension that skips decoding content
// the source has already applied. When the source's applied baseline matches
// the job's definition and filter, the coordinator calls FetchIfChanged with
// the applied hash instead of Fetch. Reporting unchanged requires the fetched
// content's hash to equal appliedHash; the Result then needs only Hash. When it
// reports changed it must behave exactly like Fetch. Wrappers that do not
// implement it are still correct; they only lose the shortcut.
// ingest.Fetcher implements it for file and Git sources.
type ChangeFetcher interface {
	Fetcher
	FetchIfChanged(ctx context.Context, source persistence.SourceDefinition, appliedHash string) (
		result ingest.Result, unchanged bool, err error)
}

// Options configures the single-worker poller. Both durations must be positive;
// LeaseDuration obeys persistence's microsecond/24h bounds. No implicit defaults.
type Options struct{ PollInterval, LeaseDuration time.Duration }

// Coordinator may be used for scheduled Run or explicit Sync (including inline
// files and operator sources). Never co-own a source with the legacy writer.
type Coordinator struct {
	jobs    persistence.Jobs
	fetcher Fetcher
	options Options
	running atomic.Bool
	ready   atomic.Bool
}

// New validates dependencies and does not start any work.
func New(jobs persistence.Jobs, fetcher Fetcher, options Options) (*Coordinator, error) {
	if jobs == nil || fetcher == nil || options.PollInterval <= 0 {
		return nil, persistence.ErrInvalid
	}
	if err := persistence.ValidateLeaseDuration(options.LeaseDuration); err != nil {
		return nil, err
	}
	return &Coordinator{jobs: jobs, fetcher: fetcher, options: options}, nil
}

// Ready reports poller readiness after its first successful claim query. Remote
// source failures do not revoke it; stopping/fatal storage failures do. Sync alone
// does not make a poller ready. Operator readiness has a stronger startup barrier.
func (c *Coordinator) Ready() bool { return c.ready.Load() }

// Run polls until cancellation (nil), or a fatal backend error. Fetch, validation
// and storage-conflict failures are safely recorded and retried on the backend's
// schedule. Even an
// immediately due failing backend cannot cause a hot loop: every attempt waits.
func (c *Coordinator) Run(ctx context.Context) error {
	if !c.running.CompareAndSwap(false, true) {
		return persistence.ErrBusy
	}
	defer c.running.Store(false)
	defer c.ready.Store(false)
	for {
		if ctx.Err() != nil {
			return nil
		}
		job, ok, err := c.jobs.ClaimNext(ctx, c.options.LeaseDuration)
		if err != nil {
			if ctx.Err() != nil {
				return shutdown.Result(err)
			}
			return err
		}
		c.ready.Store(true)
		if ok {
			retry, err := c.execute(ctx, job)
			if ctx.Err() != nil {
				return shutdown.Result(err)
			}
			if err != nil && !retry {
				return err
			}
		}
		timer := time.NewTimer(c.options.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

// Sync explicitly leases and reconciles one source. The lease is acquired before
// fetching, so definition changes and delete/recreate cannot apply stale data.
// The lease is renewed while the attempt runs, so a long fetch keeps it. A
// failure recorded on the source is returned as a *FailureError.
func (c *Coordinator) Sync(ctx context.Context, sourceName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	job, err := c.jobs.Acquire(ctx, sourceName, c.options.LeaseDuration)
	if err != nil {
		return err
	}
	_, err = c.execute(ctx, job)
	return err
}

// FailureError reports a sync attempt whose failure was recorded on the source
// with Jobs.Fail; the source keeps its previous data. Its message and
// errors.Is behave exactly as the wrapped error, which carries no remote detail.
type FailureError struct {
	Category persistence.FailureCategory
	Err      error
}

// Error implements error.
func (e *FailureError) Error() string { return e.Err.Error() }

// Unwrap returns the wrapped error.
func (e *FailureError) Unwrap() error { return e.Err }

func (c *Coordinator) execute(ctx context.Context, job persistence.Job) (bool, error) {
	work, cancel := context.WithCancel(ctx)
	source := job.Source.Name
	slog.InfoContext(ctx, "Starting sync operation", "source", source)
	renewDone := make(chan error, 1)
	go func() { renewDone <- c.renew(work, cancel, job) }()
	outcome := c.apply(work, job)
	cancel() // unblock a Renew waiting behind CommitSnapshot's source-row lock
	renewErr := <-renewDone
	category, err := outcome.category, outcome.err
	if err == nil {
		outcome.logSuccess(ctx, source)
		if renewErr != nil && !staleOnly(renewErr) {
			return false, shutdown.Result(renewErr)
		}
		return false, nil
	} // committed: cancellation or a stale fence from a blocked Renew is expected
	if ctx.Err() != nil {
		err = errors.Join(err, ctx.Err(), shutdown.Result(renewErr))
		category = persistence.FailureCancelled
	} else if renewErr != nil && (!errors.Is(renewErr, context.Canceled) || shutdown.Result(renewErr) != nil) {
		err = errors.Join(err, renewErr)
		outcome.detail = errors.Join(outcome.detail, fmt.Errorf("lease renewal: %w", renewErr))
		category = persistence.FailureStorage
	}
	if errors.Is(err, persistence.ErrStale) && staleOnly(err) {
		slog.InfoContext(ctx, "Sync result discarded: the source changed or its lease was lost", "source", source)
		return true, err
	}
	cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer done()
	if failErr := c.jobs.Fail(cleanup, job, category); failErr != nil &&
		(!errors.Is(failErr, persistence.ErrStale) || !staleOnly(failErr)) {
		logFailure(ctx, source, category, errors.Join(outcome.detail, fmt.Errorf("recording the failure: %w", failErr)))
		return false, errors.Join(err, failErr)
	}
	logFailure(ctx, source, category, outcome.detail)
	return retryable(category, err), &FailureError{Category: category, Err: err}
}

// retryable reports whether Run keeps polling after a recorded failure. Fetch and
// validation failures belong to one source. So does a storage conflict: a commit
// that lost its serialization retries to concurrent writers (publishes, another
// source's sync) or found the source recreated. Any other storage failure means
// the backend itself is unhealthy and ends Run.
func retryable(category persistence.FailureCategory, err error) bool {
	switch category {
	case persistence.FailureFetch, persistence.FailureValidation:
		return true
	case persistence.FailureStorage:
		return errors.Is(err, persistence.ErrConflict)
	case persistence.FailureCancelled:
		return false
	default:
		return false
	}
}

func staleOnly(err error) bool {
	if shutdown.Result(err) == nil {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if !staleOnly(child) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok && wrapped.Unwrap() != nil {
		return staleOnly(wrapped.Unwrap())
	}
	return errors.Is(err, persistence.ErrStale)
}

func (c *Coordinator) renew(ctx context.Context, cancel context.CancelFunc, job persistence.Job) error {
	interval := c.options.LeaseDuration / 3
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// A commit holds the same source lock as renewal. Bound that wait; cancel
			// the commit if renewal cannot finish. The backend also fences at commit.
			renewCtx, done := context.WithTimeout(ctx, interval)
			_, err := c.jobs.Renew(renewCtx, job, c.options.LeaseDuration)
			done()
			if err != nil {
				// A renewal's own timeout is a backend failure, not outer shutdown.
				if ctx.Err() == nil && shutdown.Result(err) == nil {
					err = errors.Join(persistence.ErrUnavailable, err)
				}
				cancel()
				return err
			}
		}
	}
}

// attempt is the result of one apply: the category and caller-facing error,
// the detailed cause for logs, and what a successful attempt did.
type attempt struct {
	category  persistence.FailureCategory
	err       error
	detail    error
	unchanged bool
	snapshot  model.Snapshot
	hash      string
}

func failed(category persistence.FailureCategory, err, detail error) attempt {
	if detail == nil {
		detail = err
	}
	return attempt{category: category, err: err, detail: detail}
}

func (a attempt) logSuccess(ctx context.Context, source string) {
	if a.unchanged {
		slog.DebugContext(ctx, "Registry does not need sync", "source", source, "reason", "data unchanged")
		return
	}
	hash := a.hash
	if len(hash) > 8 {
		hash = hash[:8]
	}
	slog.InfoContext(ctx, "Sync completed successfully", "source", source,
		"server_count", len(a.snapshot.Data.Servers), "skill_count", len(a.snapshot.Data.Skills),
		"plugin_count", len(a.snapshot.Data.Plugins), "hash", hash)
}

// logFailure logs a failed attempt with its category and cause. Fetch causes
// arrive redacted (ingest.Cause); URLs are redacted again here because a host
// fetcher may attach its own detail. Cancellation is routine and logged at
// Info.
func logFailure(ctx context.Context, source string, category persistence.FailureCategory, detail error) {
	cause := ""
	if detail != nil {
		cause = ingest.RedactURLs(detail.Error())
		if fetchCause := ingest.Cause(detail); fetchCause != "" {
			cause = fetchCause
		}
	}
	if category == persistence.FailureCancelled {
		slog.InfoContext(ctx, "Sync cancelled", "source", source)
		return
	}
	slog.ErrorContext(ctx, "Sync failed", "source", source, "category", string(category), "error", cause)
}

//nolint:gocyclo // Keep snapshot decisions and cancellation-aware failure classification in one attempt.
func (c *Coordinator) apply(ctx context.Context, job persistence.Job) attempt {
	filterHash, err := filterConfigHash(job.Source.Filter)
	if err != nil {
		return failed(persistence.FailureValidation, persistence.ErrInvalid, fmt.Errorf("applying the source filter: %w", err))
	}
	// status is the source's job status when it was read before fetching
	// (only for a ChangeFetcher); otherwise it is read after fetching. Either
	// way Complete and CommitSnapshot fence a baseline or lease that changed
	// since the read.
	fetched, unchanged, status, err := c.fetch(ctx, job, filterHash)
	if err != nil {
		return fetchFailure(ctx, err)
	}
	if fetched.Hash == "" {
		return failed(persistence.FailureValidation, persistence.ErrInvalid, errors.New("fetcher returned no content hash"))
	}
	if unchanged {
		result := persistence.SyncResult{Hash: fetched.Hash, FilterHash: filterHash}
		err = c.jobs.Complete(ctx, job, result)
		if err == nil {
			return attempt{unchanged: true, hash: result.Hash}
		}
		if !errors.Is(err, persistence.ErrConflict) || ctx.Err() != nil {
			return failed(persistence.FailureStorage, err, nil)
		}
		// The applied baseline changed after it was read: fetch and apply in full.
		status = nil
		if fetched, err = c.fetcher.Fetch(ctx, job.Source); err != nil {
			return fetchFailure(ctx, err)
		}
		if fetched.Hash == "" {
			return failed(persistence.FailureValidation, persistence.ErrInvalid, errors.New("fetcher returned no content hash"))
		}
	}
	snapshot, err := filterSnapshot(ctx, fetched.Snapshot, job.Source.Filter)
	if err != nil {
		return failed(persistence.FailureValidation, persistence.ErrInvalid, fmt.Errorf("applying the source filter: %w", err))
	}
	// Storage safety applies to what will be stored: a filter can exclude a
	// record that could not be stored. The fetcher already applied its
	// source's acceptance policy to the whole fetched snapshot.
	if err := persistence.ValidateStorableSnapshot(&snapshot); err != nil {
		return failed(persistence.FailureValidation, persistence.ErrInvalid, err)
	}
	result := persistence.SyncResult{Hash: fetched.Hash, FilterHash: filterHash}
	if status == nil {
		read, err := c.jobs.Status(ctx, job.Source.Name)
		if err != nil && ctx.Err() != nil {
			return failed(persistence.FailureCancelled, err, nil)
		}
		if errors.Is(err, persistence.ErrNotFound) {
			return failed(persistence.FailureStorage, persistence.ErrStale, nil)
		}
		if err != nil {
			return failed(persistence.FailureStorage, err, nil)
		}
		status = &read
	}
	if status.SourceID != job.Source.ID {
		return failed(persistence.FailureStorage, persistence.ErrStale, nil)
	}
	if status.BaselineValid &&
		status.LastHash == result.Hash && status.LastFilterHash == result.FilterHash {
		err = c.jobs.Complete(ctx, job, result)
		if err == nil {
			return attempt{unchanged: true, snapshot: snapshot, hash: result.Hash}
		}
		if !errors.Is(err, persistence.ErrConflict) || ctx.Err() != nil {
			return failed(persistence.FailureStorage, err, nil)
		}
	}
	err = c.jobs.CommitSnapshot(ctx, job, result, snapshot)
	if err == nil {
		return attempt{snapshot: snapshot, hash: result.Hash}
	}
	if errors.Is(err, persistence.ErrInvalid) && ctx.Err() == nil {
		return failed(persistence.FailureValidation, persistence.ErrInvalid, err)
	}
	return failed(persistence.FailureStorage, err, nil)
}

// fetch fetches job's source. A ChangeFetcher first gets the source's job
// status, which is returned for reuse: when the applied baseline belongs to
// this definition and filter, content matching the applied hash is reported
// unchanged without being decoded. A failed status read only loses the
// shortcut; the status is then nil and apply reads it again after fetching.
func (c *Coordinator) fetch(ctx context.Context, job persistence.Job, filterHash string) (
	ingest.Result, bool, *persistence.JobStatus, error,
) {
	changes, ok := c.fetcher.(ChangeFetcher)
	if !ok {
		fetched, err := c.fetcher.Fetch(ctx, job.Source)
		return fetched, false, nil, err
	}
	status, err := c.jobs.Status(ctx, job.Source.Name)
	if err != nil {
		fetched, err := changes.Fetch(ctx, job.Source)
		return fetched, false, nil, err
	}
	if !status.BaselineValid || status.SourceID != job.Source.ID ||
		status.LastFilterHash != filterHash || status.LastHash == "" {
		fetched, err := changes.Fetch(ctx, job.Source)
		return fetched, false, &status, err
	}
	fetched, unchanged, err := changes.FetchIfChanged(ctx, job.Source, status.LastHash)
	return fetched, unchanged, &status, err
}

// fetchFailure classifies a fetch error.
func fetchFailure(ctx context.Context, err error) attempt {
	if ctx.Err() != nil {
		return failed(persistence.FailureCancelled, errors.Join(ctx.Err(), err), nil)
	}
	if errors.Is(err, persistence.ErrStale) {
		// The fetcher captured an older definition: nothing is recorded and
		// the fetcher's own error is returned so its owner can replace it.
		return failed(persistence.FailureStorage, err, nil)
	}
	if errors.Is(err, persistence.ErrInvalid) {
		return failed(persistence.FailureValidation, persistence.ErrInvalid, err)
	}
	return failed(persistence.FailureFetch, ingest.ErrFetch, err)
}

// filterConfigHash identifies a filter configuration. A baseline applied under
// the same filter hash and content hash stored the same filtered snapshot.
func filterConfigHash(filter *persistence.Filter) (string, error) {
	data, err := json.Marshal(filter)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

func filterSnapshot(ctx context.Context, snapshot model.Snapshot, filter *persistence.Filter) (model.Snapshot, error) {
	if filter != nil && filter.Names != nil {
		for _, patterns := range [][]string{filter.Names.Include, filter.Names.Exclude} {
			for _, pattern := range patterns {
				if _, err := filepath.Match(pattern, ""); err != nil {
					return model.Snapshot{}, err
				}
				if _, err := glob.Compile(pattern); err != nil {
					return model.Snapshot{}, err
				}
			}
		}
	}
	filtered, err := filtering.NewDefaultFilterService().ApplyFilters(ctx, &snapshot, filter)
	if err != nil {
		return model.Snapshot{}, err
	}
	return *filtered, nil
}
