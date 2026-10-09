package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/sync/writer"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// Jobs borrows the migrated caller-owned pool; it never migrates or closes it.
type Jobs struct {
	pool        *pgxpool.Pool
	maxMetaSize int
}

var _ persistence.Jobs = (*Jobs)(nil)

// NewJobs does not run the global latest-pointer repair: construct NewEntries
// once after offline migration and before serving reads, not on every job poll.
func NewJobs(pool *pgxpool.Pool, maxMetaSize int) (*Jobs, error) {
	if pool == nil || maxMetaSize <= 0 {
		return nil, persistence.ErrInvalid
	}
	return &Jobs{pool: pool, maxMetaSize: maxMetaSize}, nil
}

// ClaimNext atomically acquires one due, unlocked polling source. No job means
// none was eligible and available at selection time.
//
//nolint:gocyclo // Candidate validation, legacy skips, and storage errors are independent cases.
func (d *Jobs) ClaimNext(ctx context.Context, duration time.Duration) (persistence.Job, bool, error) {
	if err := persistence.ValidateLeaseDuration(duration); err != nil {
		return persistence.Job{}, false, err
	}
	var result persistence.Job
	found := false
	err := d.write(ctx, func(tx pgx.Tx) error {
		result, found = persistence.Job{}, false // a serialization retry must not return a rolled-back claim
		q := sqlc.New(tx)
		excluded := []string{}
		for {
			name, e := q.JobNext(ctx, excluded)
			if errors.Is(e, pgx.ErrNoRows) {
				return nil
			}
			if e != nil {
				return classify(e)
			}
			excluded = append(excluded, name)
			definition, e := getSource(ctx, tx, name, false)
			if errors.Is(e, persistence.ErrInvalid) {
				continue // legacy/unsupported definition cannot be polled
			}
			if e != nil {
				return e
			}
			kind, kindErr := definition.Kind()
			if kindErr != nil || definition.Schedule == "" || kind != "git" && kind != "api" && kind != "file" ||
				definition.File != nil && definition.File.Data != "" {
				continue
			}
			row, e := q.EntrySource(ctx, uuid.MustParse(definition.ID))
			if e != nil {
				return classify(e)
			}
			if e = guardClaims(ctx, q, row); errors.Is(e, persistence.ErrConflict) {
				continue
			}
			if e != nil {
				return e
			}
			result, e = acquireTx(ctx, tx, name, duration)
			if errors.Is(e, persistence.ErrBusy) || errors.Is(e, persistence.ErrConflict) || errors.Is(e, persistence.ErrInvalid) {
				continue
			}
			if e != nil {
				return e
			}
			found = true
			return nil
		}
	})
	if err != nil {
		return persistence.Job{}, false, err
	}
	return result, found, nil
}

// Acquire explicitly starts a non-managed source, including operator/manual
// sources that ClaimNext never polls. An active lease is never stolen.
func (d *Jobs) Acquire(ctx context.Context, sourceName string, duration time.Duration) (persistence.Job, error) {
	if err := persistence.ValidateLeaseDuration(duration); err != nil {
		return persistence.Job{}, err
	}
	var job persistence.Job
	err := d.write(ctx, func(tx pgx.Tx) error {
		job = persistence.Job{}
		var e error
		job, e = acquireTx(ctx, tx, sourceName, duration)
		return e
	})
	if err != nil {
		return persistence.Job{}, err
	}
	return job, nil
}

// Renew extends a live lease from database time. Previous copies of the job
// retain the same token and remain valid; their ExpiresAt is informational.
func (d *Jobs) Renew(ctx context.Context, job persistence.Job, duration time.Duration) (persistence.Job, error) {
	if err := persistence.ValidateLeaseDuration(duration); err != nil {
		return persistence.Job{}, err
	}
	id, lease, err := jobIDs(job)
	if err != nil {
		return persistence.Job{}, err
	}
	err = d.write(ctx, func(tx pgx.Tx) error {
		if e := checkJobFence(ctx, sqlc.New(tx), id, lease, job.Generation); e != nil {
			return e
		}
		expiry, e := sqlc.New(tx).JobRenew(ctx, sqlc.JobRenewParams{
			Column1: id, Column2: lease, Column3: job.Generation, Column4: duration.Microseconds(),
		})
		if errors.Is(e, pgx.ErrNoRows) {
			return persistence.ErrStale
		}
		if e != nil {
			return classify(e)
		}
		job.ExpiresAt = expiry.Time
		return nil
	})
	return job, err
}

// Complete acknowledges a successful no-change fetch without touching entries.
func (d *Jobs) Complete(ctx context.Context, job persistence.Job, result persistence.SyncResult) error {
	id, lease, err := jobIDs(job)
	if err != nil {
		return err
	}
	return d.write(ctx, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if e := checkJobFence(ctx, q, id, lease, job.Generation); e != nil {
			return e
		}
		row, e := q.EntrySource(ctx, id)
		if e != nil {
			return classify(e)
		}
		if e = guardClaims(ctx, q, row); e != nil {
			return e
		}
		n, e := q.JobFinishUnchanged(ctx, sqlc.JobFinishUnchangedParams{
			Column1: id, Column2: lease, Column3: job.Generation, Column4: result.Hash, Column5: result.FilterHash})
		if e != nil {
			return classify(e)
		}
		if n == 0 {
			if e = checkJobFence(ctx, q, id, lease, job.Generation); e != nil {
				return e
			}
			return fmt.Errorf("%w: no applied snapshot with matching generation and hashes; commit the snapshot", persistence.ErrConflict)
		}
		return nil
	})
}

// CommitSnapshot writes all three entry kinds and the success acknowledgement
// in one serializable transaction. Failure/cancellation rolls back both and
// leaves the attempt live until Fail or expiry. The final SQL update checks
// fresh database wall time AFTER staging/COPY, not transaction-start now().
func (d *Jobs) CommitSnapshot(
	ctx context.Context, job persistence.Job, result persistence.SyncResult, snapshot model.Snapshot,
) error {
	id, lease, err := jobIDs(job)
	if err != nil {
		return err
	}
	if err = persistence.ValidateSnapshot(&snapshot); err != nil {
		return err
	}
	if err = (&Entries{maxMetaSize: d.maxMetaSize}).validateMetadata(snapshot); err != nil {
		return err
	}
	return d.write(ctx, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		if e := checkJobFence(ctx, q, id, lease, job.Generation); e != nil {
			return e
		}
		row, e := q.EntrySource(ctx, id)
		if e != nil {
			return classify(e)
		}
		kind, e := job.Source.Kind()
		if e != nil {
			return e
		}
		if row.Name != job.Source.Name || row.SourceType != kind {
			return persistence.ErrStale
		}
		if e = guardClaims(ctx, q, row); e != nil {
			return e
		}
		if e = writer.StoreSourceTx(ctx, tx, id, &snapshot, d.maxMetaSize); e != nil {
			return classify(e)
		}
		n, e := q.JobFinish(ctx, sqlc.JobFinishParams{Column1: id, Column2: lease, Column3: job.Generation,
			LastSyncHash: &result.Hash, LastAppliedFilterHash: &result.FilterHash,
			ServerCount: int64(len(snapshot.Data.Servers)),
			SkillCount:  int64(len(snapshot.Data.Skills)), PluginCount: int64(len(snapshot.Data.Plugins)),
		})
		if e != nil {
			return classify(e)
		}
		if n == 0 {
			return persistence.ErrStale
		}
		return nil
	})
}

// Fail records only a bounded category; it never persists arbitrary error text.
func (d *Jobs) Fail(ctx context.Context, job persistence.Job, category persistence.FailureCategory) error {
	if err := persistence.ValidateFailureCategory(category); err != nil {
		return err
	}
	id, lease, err := jobIDs(job)
	if err != nil {
		return err
	}
	return d.write(ctx, func(tx pgx.Tx) error {
		if e := checkJobFence(ctx, sqlc.New(tx), id, lease, job.Generation); e != nil {
			return e
		}
		message := string(category)
		n, e := sqlc.New(tx).JobFail(ctx, sqlc.JobFailParams{Column1: id, Column2: lease, Column3: job.Generation, ErrorMsg: &message})
		if e != nil {
			return classify(e)
		}
		if n == 0 {
			return persistence.ErrStale
		}
		return nil
	})
}

// Status inspects the source's latest attempt and last successful metadata.
func (d *Jobs) Status(ctx context.Context, sourceName string) (persistence.JobStatus, error) {
	row, err := sqlc.New(d.pool).JobStatus(ctx, sourceName)
	if errors.Is(err, pgx.ErrNoRows) {
		return persistence.JobStatus{}, persistence.ErrNotFound
	}
	if err != nil {
		return persistence.JobStatus{}, classify(err)
	}
	status := persistence.JobStatus{
		SourceID: row.SourceID, Phase: persistence.JobPhase(row.Phase),
		StartedAt: row.StartedAt, EndedAt: row.EndedAt,
		BaselineValid: row.BaselineValid.Valid && row.BaselineValid.Bool,
	}
	switch status.Phase {
	case persistence.JobPending, persistence.JobInProgress, persistence.JobCompleted, persistence.JobFailed:
	default:
		status.Phase = persistence.JobPending
	}
	if row.AttemptCount.Valid {
		status.Attempts = row.AttemptCount.Int64
	}
	if row.LeaseExpiresAt.Valid {
		status.LeaseExpiresAt = &row.LeaseExpiresAt.Time
	}
	if row.LastSyncHash != nil {
		status.LastHash = *row.LastSyncHash
	}
	if row.LastAppliedFilterHash != nil {
		status.LastFilterHash = *row.LastAppliedFilterHash
	}
	if row.ServerCount.Valid {
		status.ServerCount = row.ServerCount.Int64
	}
	if row.SkillCount.Valid {
		status.SkillCount = row.SkillCount.Int64
	}
	if row.PluginCount.Valid {
		status.PluginCount = row.PluginCount.Int64
	}
	if row.ErrorMsg != nil && persistence.ValidateFailureCategory(persistence.FailureCategory(*row.ErrorMsg)) == nil {
		status.Failure = persistence.FailureCategory(*row.ErrorMsg)
	}
	return status, nil
}

func (d *Jobs) write(ctx context.Context, fn func(pgx.Tx) error) error {
	return writeTx(ctx, d.pool, false, fn)
}

func acquireTx(ctx context.Context, tx pgx.Tx, name string, duration time.Duration) (persistence.Job, error) {
	q := sqlc.New(tx)
	source, err := q.JobLockSource(ctx, name)
	if errors.Is(err, pgx.ErrNoRows) {
		return persistence.Job{}, persistence.ErrNotFound
	}
	if err != nil {
		return persistence.Job{}, classify(err)
	}
	definition, err := getSource(ctx, tx, name, false)
	if err != nil {
		return persistence.Job{}, err
	}
	if err = requireKnownFields(ctx, tx, name, definition); err != nil {
		return persistence.Job{}, err
	}
	kind, err := definition.Kind()
	if err != nil {
		return persistence.Job{}, err
	}
	if kind == managedSourceKind {
		return persistence.Job{}, fmt.Errorf("%w: managed source cannot be leased", persistence.ErrConflict)
	}
	row, err := q.JobAcquire(ctx, sqlc.JobAcquireParams{
		Column1: source.ID, Column2: source.DefinitionGeneration, Column3: duration.Microseconds(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return persistence.Job{}, persistence.ErrBusy
	}
	if err != nil {
		return persistence.Job{}, classify(err)
	}
	return persistence.Job{
		Source: definition, LeaseID: row.LeaseID.String(), Generation: source.DefinitionGeneration, ExpiresAt: row.LeaseExpiresAt.Time,
	}, nil
}

func checkJobFence(ctx context.Context, q *sqlc.Queries, id, lease uuid.UUID, generation int64) error {
	_, err := q.JobFence(ctx, sqlc.JobFenceParams{Column1: id, Column2: lease, Column3: generation})
	if errors.Is(err, pgx.ErrNoRows) {
		return persistence.ErrStale
	}
	return classify(err)
}

func jobIDs(job persistence.Job) (uuid.UUID, uuid.UUID, error) {
	id, e := uuid.Parse(job.Source.ID)
	if e != nil {
		return uuid.UUID{}, uuid.UUID{}, persistence.ErrInvalid
	}
	lease, e := uuid.Parse(job.LeaseID)
	if e != nil || job.Generation <= 0 {
		return uuid.UUID{}, uuid.UUID{}, persistence.ErrInvalid
	}
	return id, lease, nil
}
