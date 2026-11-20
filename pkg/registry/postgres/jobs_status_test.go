package postgres_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

// syncRow is the part of registry_sync that /v1/sources reports.
type syncRow struct {
	Status    string
	Message   *string
	StartedAt *time.Time
	EndedAt   *time.Time
	Attempts  int64
	CheckedAt *time.Time
}

func readSyncRow(t *testing.T, pool *pgxpool.Pool, sourceID string) syncRow {
	t.Helper()
	var r syncRow
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT sync_status::text, error_msg, started_at, ended_at,
 attempt_count, checked_at FROM registry_sync WHERE source_id=$1`, sourceID).Scan(
		&r.Status, &r.Message, &r.StartedAt, &r.EndedAt, &r.Attempts, &r.CheckedAt))
	return r
}

// ageSync makes the source due by moving its last finish and check an hour back.
func ageSync(t *testing.T, pool *pgxpool.Pool, sourceID string) {
	t.Helper()
	_, err := pool.Exec(t.Context(), `UPDATE registry_sync SET ended_at=ended_at-interval '1 hour',
 started_at=started_at-interval '1 hour', checked_at=checked_at-interval '1 hour' WHERE source_id=$1`, sourceID)
	require.NoError(t, err)
}

// An attempt keeps the previous finish time and message visible while it runs.
// A changed commit and a failure record the attempt; an unchanged fetch after a
// success moves only the due time (v1.5.4 wrote nothing then); an attempt
// whose lease expires reports, and is retried from, the state it replaced.
//
//nolint:gocyclo // One table drives four outcomes from two prior states.
func TestJobStatusAcrossPollOutcomes(t *testing.T) {
	t.Parallel()
	const hash, filterHash = "H", "F"
	changedServer := model.Snapshot{}
	changedServer.Data.Servers = []model.Server{{
		Schema: "https://example.org/server.schema.json", Name: "com.example/server", Version: "2", Description: "changed",
	}}
	for _, tc := range []struct {
		name        string
		priorFailed bool
		outcome     string // changed, unchanged, failed, expired
	}{
		{name: "after success, changed", outcome: "changed"},
		{name: "after success, unchanged", outcome: "unchanged"},
		{name: "after success, failed", outcome: "failed"},
		{name: "after success, lease expired", outcome: "expired"},
		{name: "after failure, changed", priorFailed: true, outcome: "changed"},
		{name: "after failure, unchanged", priorFailed: true, outcome: "unchanged"},
		{name: "after failure, failed", priorFailed: true, outcome: "failed"},
		{name: "after failure, lease expired", priorFailed: true, outcome: "expired"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d, pool := newBackend(t)
			jobs, err := postgres.NewJobs(pool, 65536)
			require.NoError(t, err)
			s, err := d.CreateSource(t.Context(), persistence.SourceDefinition{
				Name: "polled", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "30m",
			})
			require.NoError(t, err)

			first, ok, err := jobs.ClaimNext(t.Context(), time.Minute)
			require.NoError(t, err)
			require.True(t, ok)
			require.NoError(t, jobs.CommitSnapshot(t.Context(), first, persistence.SyncResult{Hash: hash, FilterHash: filterHash}, model.Snapshot{}))
			ageSync(t, pool, s.ID)
			if tc.priorFailed {
				job, ok, err := jobs.ClaimNext(t.Context(), time.Minute)
				require.NoError(t, err)
				require.True(t, ok)
				require.NoError(t, jobs.Fail(t.Context(), job, persistence.FailureFetch))
				ageSync(t, pool, s.ID)
			}
			before := readSyncRow(t, pool, s.ID)

			lease := time.Minute
			if tc.outcome == "expired" {
				lease = 200 * time.Millisecond
			}
			job, ok, err := jobs.ClaimNext(t.Context(), lease)
			require.NoError(t, err)
			require.True(t, ok, "an aged source is due")

			// While the attempt runs, the previous finish time and message stay.
			during := readSyncRow(t, pool, s.ID)
			require.Equal(t, "IN_PROGRESS", during.Status)
			require.Equal(t, before.EndedAt, during.EndedAt, "lastSyncTime during a sync")
			require.Equal(t, before.Message, during.Message, "message during a sync")
			require.Equal(t, before.Attempts+1, during.Attempts)
			status, err := jobs.Status(t.Context(), s.Name)
			require.NoError(t, err)
			require.Equal(t, persistence.JobInProgress, status.Phase)
			require.Equal(t, before.EndedAt, status.EndedAt)
			if tc.priorFailed {
				require.Equal(t, persistence.FailureFetch, status.Failure)
			}

			switch tc.outcome {
			case "changed":
				require.NoError(t, jobs.CommitSnapshot(t.Context(), job, persistence.SyncResult{Hash: "H2", FilterHash: filterHash}, changedServer))
				after := readSyncRow(t, pool, s.ID)
				require.Equal(t, "COMPLETED", after.Status)
				require.Nil(t, after.Message)
				require.Zero(t, after.Attempts)
				require.True(t, after.EndedAt.After(*before.EndedAt))
				require.True(t, after.StartedAt.After(*before.StartedAt))
			case "unchanged":
				require.NoError(t, jobs.Complete(t.Context(), job, persistence.SyncResult{Hash: hash, FilterHash: filterHash}))
				after := readSyncRow(t, pool, s.ID)
				require.True(t, after.CheckedAt.After(*before.CheckedAt), "the due time moves")
				if tc.priorFailed {
					// A success after a failure is recorded as one.
					require.Equal(t, "COMPLETED", after.Status)
					require.Nil(t, after.Message)
					require.Zero(t, after.Attempts)
					require.True(t, after.EndedAt.After(*before.EndedAt))
				} else {
					after.CheckedAt, before.CheckedAt = nil, nil
					require.Equal(t, before, after, "an unchanged poll after a success leaves the status alone")
				}
			case "failed":
				require.NoError(t, jobs.Fail(t.Context(), job, persistence.FailureFetch))
				after := readSyncRow(t, pool, s.ID)
				require.Equal(t, "FAILED", after.Status)
				require.Equal(t, string(persistence.FailureFetch), *after.Message)
				require.Equal(t, before.Attempts+1, after.Attempts)
				require.True(t, after.EndedAt.After(*before.EndedAt))
			case "expired":
				time.Sleep(time.Until(job.ExpiresAt) + 50*time.Millisecond)
				require.ErrorIs(t, jobs.Complete(t.Context(), job, persistence.SyncResult{Hash: hash, FilterHash: filterHash}), persistence.ErrStale)
				status, err := jobs.Status(t.Context(), s.Name)
				require.NoError(t, err)
				require.Equal(t, persistence.JobPhase(before.Status), status.Phase, "an expired attempt is not IN_PROGRESS")
				require.Equal(t, before.StartedAt, status.StartedAt)
				require.Equal(t, before.EndedAt, status.EndedAt)
				require.Equal(t, before.Attempts, status.Attempts)
				// It is retried at once and the retry still sees the earlier state.
				retry, ok, err := jobs.ClaimNext(t.Context(), time.Minute)
				require.NoError(t, err)
				require.True(t, ok, "an expired attempt is due again")
				require.NoError(t, jobs.Complete(t.Context(), retry, persistence.SyncResult{Hash: hash, FilterHash: filterHash}))
				after := readSyncRow(t, pool, s.ID)
				if tc.priorFailed {
					require.Equal(t, "COMPLETED", after.Status)
				} else {
					after.CheckedAt, before.CheckedAt = nil, nil
					require.Equal(t, before, after)
				}
			}
			// Whatever the outcome, a finished attempt is not due again at once.
			_, ok, err = jobs.ClaimNext(t.Context(), time.Minute)
			require.NoError(t, err)
			if tc.outcome == "failed" {
				return // a failure retries after FailedRetryInterval; covered elsewhere
			}
			require.False(t, ok, "not due right after the poll")
		})
	}
}
