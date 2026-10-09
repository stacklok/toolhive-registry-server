package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
)

var (
	// ErrBusy indicates another worker holds a live lease for this source.
	ErrBusy = errors.New("source lease busy")
	// ErrStale indicates that a lease expired, was consumed, or its source changed.
	ErrStale = errors.New("sync job stale")
)

// Job carries an opaque lease identity and the source definition captured at claim.
// Renew returns a new expiry; the lease identity remains unchanged. DB time, not
// this value or the caller's clock, is authoritative at each write.
type Job struct {
	Source     SourceDefinition
	LeaseID    string
	Generation int64
	ExpiresAt  time.Time
}

// JobPhase is the latest attempt's state, not the validity of its catalog baseline.
type JobPhase string

// Job phases describe the latest attempt; they do not indicate baseline validity.
const (
	JobPending    JobPhase = "PENDING"
	JobInProgress JobPhase = "IN_PROGRESS"
	JobCompleted  JobPhase = "COMPLETED"
	JobFailed     JobPhase = "FAILED"
)

// JobStatus reports the most recent attempt and the last successful snapshot.
// Failure preserves successful metadata, but BaselineValid is false when the
// definition has changed. Direct replacement clears the prior baseline.
// A zero Failure means no categorized error (including unknown legacy errors).
type JobStatus struct {
	SourceID       string
	Phase          JobPhase
	BaselineValid  bool
	Attempts       int64
	StartedAt      *time.Time
	EndedAt        *time.Time
	LeaseExpiresAt *time.Time
	LastHash       string
	LastFilterHash string
	ServerCount    int64
	SkillCount     int64
	PluginCount    int64
	Failure        FailureCategory
}

// SyncResult identifies the applied fetch. Complete accepts it only when the
// current generation already has an applied snapshot with identical hashes;
// otherwise it returns ErrConflict without consuming the lease. An initial or
// changed fetch (including an empty snapshot) must use CommitSnapshot.
type SyncResult struct{ Hash, FilterHash string }

// FailureCategory is intentionally a small allowlist, not an arbitrary error
// message: fetch errors and source definitions may contain credentials.
type FailureCategory string

// Failure categories are the only error details persisted for jobs.
const (
	FailureFetch      FailureCategory = "fetch"
	FailureValidation FailureCategory = "validation"
	FailureStorage    FailureCategory = "storage"
	FailureCancelled  FailureCategory = "cancelled"
)

// Jobs coordinates source-scoped, expiring leases and atomic snapshot acknowledgements.
// ClaimNext polls only scheduled git/API/file sources; Acquire(ctx, sourceName, duration)
// may explicitly lease any non-managed source, including Kubernetes and inline files.
// Status(ctx, sourceName) also looks up by name. Job.Source.ID is the immutable
// source incarnation used for fencing, not the lookup name. Explicit Acquire
// can wait for another transaction's source-row lock; ClaimNext skips locked rows.
// The standalone coordinator is an exclusive alternative owner, not a co-worker.
type Jobs interface {
	ClaimNext(context.Context, time.Duration) (Job, bool, error)
	Acquire(ctx context.Context, sourceName string, duration time.Duration) (Job, error)
	Renew(context.Context, Job, time.Duration) (Job, error)
	Complete(context.Context, Job, SyncResult) error
	CommitSnapshot(context.Context, Job, SyncResult, model.Snapshot) error
	Fail(context.Context, Job, FailureCategory) error
	Status(ctx context.Context, sourceName string) (JobStatus, error)
}

// ValidateLeaseDuration limits lease durations to positive whole microseconds
// (PostgreSQL timestamp precision) at most 24 hours; zero is never indefinite.
func ValidateLeaseDuration(d time.Duration) error {
	if d <= 0 || d > 24*time.Hour || d%time.Microsecond != 0 {
		return fmt.Errorf("%w: lease duration must be positive whole microseconds, at most 24h", ErrInvalid)
	}
	return nil
}

// ValidateFailureCategory prohibits persisting arbitrary caller-controlled errors.
func ValidateFailureCategory(c FailureCategory) error {
	switch c {
	case FailureFetch, FailureValidation, FailureStorage, FailureCancelled:
		return nil
	}
	return fmt.Errorf("%w: unknown failure category", ErrInvalid)
}
