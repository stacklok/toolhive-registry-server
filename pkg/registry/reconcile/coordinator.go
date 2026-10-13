// Package reconcile coordinates claims-free source ingestion over persistence.Jobs.
// It never owns a database, listener, identity policy or supplied client.
package reconcile

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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

//go:generate mockgen -destination=mocks/fetcher.go -package=mocks . Fetcher
//go:generate mockgen -destination=mocks/jobs.go -package=mocks github.com/stacklok/toolhive-registry-server/pkg/registry/persistence Jobs

// Fetcher must return a complete snapshot, never partial data on failure. It must
// honor cancellation and must not mutate the captured definition.
type Fetcher interface {
	Fetch(context.Context, persistence.SourceDefinition) (ingest.Result, error)
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

// Run polls until cancellation (nil), or a fatal backend error. Fetch/validation
// failures are safely recorded and retried on the backend's schedule. Even an
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

func (c *Coordinator) execute(ctx context.Context, job persistence.Job) (bool, error) {
	work, cancel := context.WithCancel(ctx)
	renewDone := make(chan error, 1)
	go func() { renewDone <- c.renew(work, cancel, job) }()
	category, err := c.apply(work, job)
	cancel() // unblock a Renew waiting behind CommitSnapshot's source-row lock
	renewErr := <-renewDone
	if err == nil {
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
		category = persistence.FailureStorage
	}
	if errors.Is(err, persistence.ErrStale) && staleOnly(err) {
		return true, err
	}
	cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer done()
	if failErr := c.jobs.Fail(cleanup, job, category); failErr != nil &&
		(!errors.Is(failErr, persistence.ErrStale) || !staleOnly(failErr)) {
		return false, errors.Join(err, failErr)
	}
	return category == persistence.FailureFetch || category == persistence.FailureValidation, err
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
				cancel()
				return err
			}
		}
	}
}

//nolint:gocyclo // Keep snapshot decisions and cancellation-aware failure classification in one attempt.
func (c *Coordinator) apply(ctx context.Context, job persistence.Job) (persistence.FailureCategory, error) {
	fetched, err := c.fetcher.Fetch(ctx, job.Source)
	if err != nil {
		if ctx.Err() != nil {
			return persistence.FailureCancelled, errors.Join(ctx.Err(), err)
		}
		if errors.Is(err, persistence.ErrInvalid) {
			return persistence.FailureValidation, persistence.ErrInvalid
		}
		return persistence.FailureFetch, ingest.ErrFetch
	}
	if fetched.Hash == "" {
		return persistence.FailureValidation, persistence.ErrInvalid
	}
	// Validate the entire unfiltered snapshot, including records excluded later.
	if err := persistence.ValidateSnapshot(&fetched.Snapshot); err != nil {
		return persistence.FailureValidation, err
	}
	snapshot, filterHash, err := filterSnapshot(ctx, fetched.Snapshot, job.Source.Filter)
	if err != nil {
		return persistence.FailureValidation, persistence.ErrInvalid
	}
	result := persistence.SyncResult{Hash: fetched.Hash, FilterHash: filterHash}
	status, err := c.jobs.Status(ctx, job.Source.Name)
	if err != nil && ctx.Err() != nil {
		return persistence.FailureCancelled, err
	}
	if errors.Is(err, persistence.ErrNotFound) {
		return persistence.FailureStorage, persistence.ErrStale
	}
	if err != nil {
		return persistence.FailureStorage, err
	}
	if status.SourceID != job.Source.ID {
		return persistence.FailureStorage, persistence.ErrStale
	}
	if status.BaselineValid &&
		status.LastHash == result.Hash && status.LastFilterHash == result.FilterHash {
		err = c.jobs.Complete(ctx, job, result)
		if !errors.Is(err, persistence.ErrConflict) || ctx.Err() != nil {
			return persistence.FailureStorage, err
		}
	}
	err = c.jobs.CommitSnapshot(ctx, job, result, snapshot)
	if errors.Is(err, persistence.ErrInvalid) && ctx.Err() == nil {
		return persistence.FailureValidation, persistence.ErrInvalid
	}
	return persistence.FailureStorage, err
}

func filterSnapshot(ctx context.Context, snapshot model.Snapshot, filter *persistence.Filter) (model.Snapshot, string, error) {
	if filter != nil && filter.Names != nil {
		for _, patterns := range [][]string{filter.Names.Include, filter.Names.Exclude} {
			for _, pattern := range patterns {
				if _, err := filepath.Match(pattern, ""); err != nil {
					return model.Snapshot{}, "", err
				}
				if _, err := glob.Compile(pattern); err != nil {
					return model.Snapshot{}, "", err
				}
			}
		}
	}
	data, err := json.Marshal(filter)
	if err != nil {
		return model.Snapshot{}, "", err
	}
	filtered, err := filtering.NewDefaultFilterService().ApplyFilters(ctx, &snapshot, filter)
	if err != nil {
		return model.Snapshot{}, "", err
	}
	return *filtered, fmt.Sprintf("%x", sha256.Sum256(data)), nil
}
