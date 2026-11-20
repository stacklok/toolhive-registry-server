package reconcile_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
)

// lockedBuffer collects log output written from several goroutines.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// records returns the logged records with the given message.
func (b *lockedBuffer) records(t *testing.T, msg string) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(b.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &record))
		if record["msg"] == msg {
			out = append(out, record)
		}
	}
	return out
}

// captureLogs swaps the default logger. Tests using it must not be parallel:
// Go runs them before any parallel test of the package resumes.
func captureLogs(t *testing.T) *lockedBuffer {
	t.Helper()
	logs := &lockedBuffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return logs
}

//nolint:paralleltest // Swaps the process-wide default logger.
func TestSyncLogsOutcome(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*mocks.MockJobs, *mocks.MockFetcher, persistence.Job)
		check func(*testing.T, *lockedBuffer, error)
	}{
		{
			name: "committed",
			setup: func(jobs *mocks.MockJobs, fetcher *mocks.MockFetcher, job persistence.Job) {
				fetcher.EXPECT().Fetch(gomock.Any(), job.Source).Return(ingest.Result{Snapshot: snapshotFixture(), Hash: "0123456789abcdef"}, nil)
				jobs.EXPECT().Status(gomock.Any(), job.Source.Name).Return(persistence.JobStatus{SourceID: job.Source.ID}, nil)
				jobs.EXPECT().CommitSnapshot(gomock.Any(), job, gomock.Any(), gomock.Any()).Return(nil)
			},
			check: func(t *testing.T, logs *lockedBuffer, err error) {
				t.Helper()
				require.NoError(t, err)
				require.Len(t, logs.records(t, "Starting sync operation"), 1)
				done := logs.records(t, "Sync completed successfully")
				require.Len(t, done, 1)
				require.Equal(t, "INFO", done[0]["level"])
				require.Equal(t, "test", done[0]["source"])
				require.EqualValues(t, 1, done[0]["server_count"])
				require.EqualValues(t, 1, done[0]["skill_count"])
				require.EqualValues(t, 1, done[0]["plugin_count"])
				require.Equal(t, "01234567", done[0]["hash"])
			},
		},
		{
			name: "unchanged",
			setup: func(jobs *mocks.MockJobs, fetcher *mocks.MockFetcher, job persistence.Job) {
				fetcher.EXPECT().Fetch(gomock.Any(), job.Source).Return(ingest.Result{Snapshot: snapshotFixture(), Hash: "h"}, nil)
				jobs.EXPECT().Status(gomock.Any(), job.Source.Name).Return(persistence.JobStatus{
					SourceID: job.Source.ID, BaselineValid: true, LastHash: "h", LastFilterHash: "74234e98afe7498fb5daf1f36ac2d78acc339464f950703b8c019892f982b90b",
				}, nil)
				jobs.EXPECT().Complete(gomock.Any(), job, gomock.Any()).Return(nil)
			},
			check: func(t *testing.T, logs *lockedBuffer, err error) {
				t.Helper()
				require.NoError(t, err)
				unchanged := logs.records(t, "Registry does not need sync")
				require.Len(t, unchanged, 1)
				require.Equal(t, "DEBUG", unchanged[0]["level"])
				require.Empty(t, logs.records(t, "Sync completed successfully"))
			},
		},
		{
			name: "fetch failure logs the redacted cause",
			setup: func(jobs *mocks.MockJobs, fetcher *mocks.MockFetcher, job persistence.Job) {
				cause := errors.New(`Get "https://bot:hunter2@example.org/v0.1/servers?token=abc": connection refused`)
				fetcher.EXPECT().Fetch(gomock.Any(), job.Source).Return(ingest.Result{}, ingest.WithCause(ingest.ErrFetch, cause))
				jobs.EXPECT().Fail(gomock.Any(), job, persistence.FailureFetch).Return(nil)
			},
			check: func(t *testing.T, logs *lockedBuffer, err error) {
				t.Helper()
				require.ErrorIs(t, err, ingest.ErrFetch)
				require.Equal(t, ingest.ErrFetch.Error(), err.Error(), "the returned error stays a category")
				failed := logs.records(t, "Sync failed")
				require.Len(t, failed, 1)
				require.Equal(t, "ERROR", failed[0]["level"])
				require.Equal(t, "fetch", failed[0]["category"])
				require.Equal(t, `Get "https://example.org/v0.1/servers": connection refused`, failed[0]["error"])
			},
		},
		{
			name: "validation failure names the record",
			setup: func(jobs *mocks.MockJobs, fetcher *mocks.MockFetcher, job persistence.Job) {
				snapshot := snapshotFixture()
				snapshot.Data.Servers[0].Name = "no-slash"
				fetcher.EXPECT().Fetch(gomock.Any(), job.Source).Return(ingest.Result{Snapshot: snapshot, Hash: "h"}, nil)
				jobs.EXPECT().Fail(gomock.Any(), job, persistence.FailureValidation).Return(nil)
			},
			check: func(t *testing.T, logs *lockedBuffer, err error) {
				t.Helper()
				require.ErrorIs(t, err, persistence.ErrInvalid)
				require.Equal(t, persistence.ErrInvalid.Error(), err.Error())
				failed := logs.records(t, "Sync failed")
				require.Len(t, failed, 1)
				require.Equal(t, "validation", failed[0]["category"])
				require.Contains(t, failed[0]["error"], `server "no-slash" version "1"`)
				require.Contains(t, failed[0]["error"], "namespace/name")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLogs(t)
			ctrl := gomock.NewController(t)
			jobs, fetcher := mocks.NewMockJobs(ctrl), mocks.NewMockFetcher(ctrl)
			job := jobFixture()
			jobs.EXPECT().Acquire(gomock.Any(), job.Source.Name, time.Minute).Return(job, nil)
			tc.setup(jobs, fetcher, job)
			err := newCoordinator(t, jobs, fetcher, time.Minute).Sync(t.Context(), job.Source.Name)
			tc.check(t, logs, err)
		})
	}
}
