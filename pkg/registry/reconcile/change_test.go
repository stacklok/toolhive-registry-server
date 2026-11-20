package reconcile_test

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
)

// TestChangeFetcherShortcut covers when the coordinator asks a ChangeFetcher
// to skip decoding, and that every other case still applies content in full.
func TestChangeFetcherShortcut(t *testing.T) {
	t.Parallel()
	filterHash := fmt.Sprintf("%x", sha256.Sum256([]byte("null")))
	applied := persistence.JobStatus{SourceID: "incarnation", BaselineValid: true, LastHash: "hash", LastFilterHash: filterHash}
	invalidated := applied
	invalidated.BaselineValid = false

	type expect struct {
		jobs    *mocks.MockJobs
		fetcher *mocks.MockChangeFetcher
		job     persistence.Job
	}
	full := ingest.Result{Snapshot: snapshotFixture(), Hash: "hash"}
	changed := ingest.Result{Snapshot: snapshotFixture(), Hash: "new"}

	for _, tc := range []struct {
		name    string
		setup   func(e expect)
		wantErr error
	}{
		{
			name: "unchanged content completes without decoding or committing",
			setup: func(e expect) {
				e.jobs.EXPECT().Status(gomock.Any(), e.job.Source.Name).Return(applied, nil)
				e.fetcher.EXPECT().FetchIfChanged(gomock.Any(), e.job.Source, "hash").Return(ingest.Result{Hash: "hash"}, true, nil)
				e.jobs.EXPECT().Complete(gomock.Any(), e.job, persistence.SyncResult{Hash: "hash", FilterHash: filterHash}).Return(nil)
			},
		},
		{
			name: "changed content is committed",
			setup: func(e expect) {
				// The status read before fetching is reused: one read per attempt.
				e.jobs.EXPECT().Status(gomock.Any(), e.job.Source.Name).Return(applied, nil)
				e.fetcher.EXPECT().FetchIfChanged(gomock.Any(), e.job.Source, "hash").Return(changed, false, nil)
				e.jobs.EXPECT().CommitSnapshot(gomock.Any(), e.job, persistence.SyncResult{Hash: "new", FilterHash: filterHash}, gomock.Any()).Return(nil)
			},
		},
		{
			name: "a baseline that moved after it was read falls back to a full fetch",
			setup: func(e expect) {
				e.jobs.EXPECT().Status(gomock.Any(), e.job.Source.Name).Return(applied, nil)
				e.fetcher.EXPECT().FetchIfChanged(gomock.Any(), e.job.Source, "hash").Return(ingest.Result{Hash: "hash"}, true, nil)
				e.jobs.EXPECT().Complete(gomock.Any(), e.job, gomock.Any()).Return(persistence.ErrConflict)
				e.fetcher.EXPECT().Fetch(gomock.Any(), e.job.Source).Return(full, nil)
				e.jobs.EXPECT().Status(gomock.Any(), e.job.Source.Name).Return(invalidated, nil)
				e.jobs.EXPECT().CommitSnapshot(gomock.Any(), e.job, persistence.SyncResult{Hash: "hash", FilterHash: filterHash}, gomock.Any()).Return(nil)
			},
		},
		{
			name: "an unchanged Complete storage failure is recorded",
			setup: func(e expect) {
				e.jobs.EXPECT().Status(gomock.Any(), e.job.Source.Name).Return(applied, nil)
				e.fetcher.EXPECT().FetchIfChanged(gomock.Any(), e.job.Source, "hash").Return(ingest.Result{Hash: "hash"}, true, nil)
				e.jobs.EXPECT().Complete(gomock.Any(), e.job, gomock.Any()).Return(persistence.ErrUnavailable)
				e.jobs.EXPECT().Fail(gomock.Any(), e.job, persistence.FailureStorage).Return(nil)
			},
			wantErr: persistence.ErrUnavailable,
		},
		{
			name: "a fetch failure is recorded",
			setup: func(e expect) {
				e.jobs.EXPECT().Status(gomock.Any(), e.job.Source.Name).Return(applied, nil)
				e.fetcher.EXPECT().FetchIfChanged(gomock.Any(), e.job.Source, "hash").Return(ingest.Result{}, false, errors.New("password=secret"))
				e.jobs.EXPECT().Fail(gomock.Any(), e.job, persistence.FailureFetch).Return(nil)
			},
			wantErr: ingest.ErrFetch,
		},
		{
			name: "an unchanged result without a hash is invalid",
			setup: func(e expect) {
				e.jobs.EXPECT().Status(gomock.Any(), e.job.Source.Name).Return(applied, nil)
				e.fetcher.EXPECT().FetchIfChanged(gomock.Any(), e.job.Source, "hash").Return(ingest.Result{}, true, nil)
				e.jobs.EXPECT().Fail(gomock.Any(), e.job, persistence.FailureValidation).Return(nil)
			},
			wantErr: persistence.ErrInvalid,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			e := expect{jobs: mocks.NewMockJobs(ctrl), fetcher: mocks.NewMockChangeFetcher(ctrl), job: jobFixture()}
			e.jobs.EXPECT().Acquire(gomock.Any(), e.job.Source.Name, time.Minute).Return(e.job, nil)
			tc.setup(e)
			err := newCoordinator(t, e.jobs, e.fetcher, time.Minute).Sync(t.Context(), e.job.Source.Name)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
			require.NotContains(t, err.Error(), "secret")
		})
	}
}

// TestChangeFetcherNeedsAMatchingBaseline covers the baselines that must not be
// trusted: the coordinator then fetches and decodes in full, reusing the status
// it read unless that read failed.
func TestChangeFetcherNeedsAMatchingBaseline(t *testing.T) {
	t.Parallel()
	filterHash := fmt.Sprintf("%x", sha256.Sum256([]byte("null")))
	applied := persistence.JobStatus{SourceID: "incarnation", BaselineValid: true, LastHash: "hash", LastFilterHash: filterHash}
	invalidated := applied
	invalidated.BaselineValid = false
	otherIncarnation := applied
	otherIncarnation.SourceID = "previous"
	otherFilter := applied
	otherFilter.LastFilterHash = "other"
	noHash := applied
	noHash.LastHash = ""

	for _, tc := range []struct {
		name      string
		status    persistence.JobStatus
		statusErr error
		wantErr   error
	}{
		{name: "no valid baseline", status: invalidated},
		{name: "a baseline applied under another filter", status: otherFilter},
		{name: "a baseline without a hash", status: noHash},
		{name: "a recreated source is stale", status: otherIncarnation, wantErr: persistence.ErrStale},
		{name: "an unreadable status is read again after fetching", statusErr: persistence.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			jobs := mocks.NewMockJobs(ctrl)
			fetcher := mocks.NewMockChangeFetcher(ctrl)
			job := jobFixture()
			jobs.EXPECT().Acquire(gomock.Any(), job.Source.Name, time.Minute).Return(job, nil)
			jobs.EXPECT().Status(gomock.Any(), job.Source.Name).Return(tc.status, tc.statusErr)
			fetcher.EXPECT().Fetch(gomock.Any(), job.Source).Return(ingest.Result{Snapshot: snapshotFixture(), Hash: "hash"}, nil)
			if tc.statusErr != nil {
				jobs.EXPECT().Status(gomock.Any(), job.Source.Name).Return(persistence.JobStatus{SourceID: job.Source.ID}, nil)
			}
			if tc.wantErr == nil {
				jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).Return(nil)
			}
			err := newCoordinator(t, jobs, fetcher, time.Minute).Sync(t.Context(), job.Source.Name)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}
