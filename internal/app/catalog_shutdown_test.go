package app

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/app/storage"
	storagemocks "github.com/stacklok/toolhive-registry-server/internal/app/storage/mocks"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	fetchmocks "github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
)

func TestCatalogStopPreservesActiveSyncFailure(t *testing.T) {
	t.Parallel()
	cfg := catalogTestConfig(t)
	conn, pool := catalogPG(t, cfg)
	cfg.Sources = append(cfg.Sources, config.SourceConfig{Name: "inline", File: &config.FileConfig{Data: `{"version":"1.0.0","meta":{"last_updated":"2025-01-15T10:30:00Z"},"data":{"servers":[{"$schema":"https://example.org/server.schema.json","name":"com.example/inline","description":"fixture","version":"1"}]}}`}})
	approveCatalog(t, conn, cfg, true)
	backend, err := (storage.CatalogPostgres{BorrowedPool: pool}).Open(t.Context(), cfg)
	require.NoError(t, err)
	ctrl := gomock.NewController(t)
	factory, store := storagemocks.NewMockCatalogFactory(ctrl), storagemocks.NewMockCatalogStorage(ctrl)
	jobs := fetchmocks.NewMockJobs(ctrl)
	var job persistence.Job
	factory.EXPECT().Open(gomock.Any(), gomock.Any()).Return(store, nil)
	store.EXPECT().Preflight(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
	store.EXPECT().Consume(gomock.Any(), catalogRevision(t, cfg)).DoAndReturn(backend.Consume)
	store.EXPECT().Initialize(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, s []persistence.SourceDefinition, v []persistence.ViewDefinition) (storage.CatalogComponents, error) {
		components, err := backend.Initialize(ctx, s, v)
		if err != nil {
			return components, err
		}
		definitions, err := components.Definitions.ListSources(ctx)
		if err != nil {
			return components, err
		}
		for _, d := range definitions {
			if d.Name == "inline" {
				job = persistence.Job{Source: d, LeaseID: "lease", Generation: 1}
			}
		}
		components.Jobs = jobs
		return components, nil
	})
	closed := make(chan struct{})
	store.EXPECT().Close().Do(func() { backend.Close(); close(closed) })
	jobs.EXPECT().ClaimNext(gomock.Any(), time.Minute).Return(persistence.Job{}, false, nil).AnyTimes()
	first := jobs.EXPECT().Status(gomock.Any(), "inline").DoAndReturn(func(context.Context, string) (persistence.JobStatus, error) {
		return persistence.JobStatus{SourceID: job.Source.ID}, nil
	})
	jobs.EXPECT().Acquire(gomock.Any(), "inline", time.Minute).After(first).DoAndReturn(func(context.Context, string, time.Duration) (persistence.Job, error) {
		return job, nil
	})
	entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	backendErr := errors.New("active storage failure")
	jobs.EXPECT().Status(gomock.Any(), "inline").After(first).DoAndReturn(func(ctx context.Context, _ string) (persistence.JobStatus, error) {
		close(entered)
		<-ctx.Done()
		close(canceled)
		<-release
		return persistence.JobStatus{}, fmt.Errorf("status: %w", errors.Join(ctx.Err(), backendErr))
	})
	jobs.EXPECT().Fail(gomock.Any(), gomock.Any(), persistence.FailureCancelled).DoAndReturn(func(ctx context.Context, _ persistence.Job, _ persistence.FailureCategory) error {
		require.NoError(t, ctx.Err())
		return nil
	})
	app, err := NewCatalogApp(t.Context(), CatalogOptions{Config: cfg, Storage: factory, HTTP: []RegistryAppOptions{WithAddress("127.0.0.1:0"), WithInternalAddress("127.0.0.1:0")}})
	require.NoError(t, err)
	require.NoError(t, app.Start())
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("application did not enter active Sync")
	}
	stopped := make(chan error, 1)
	go func() { stopped <- app.Stop() }()
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("application did not cancel Sync")
	}
	select {
	case <-closed:
		t.Fatal("storage closed before active Sync joined")
	default:
	}
	close(release)
	select {
	case err := <-stopped:
		require.ErrorIs(t, err, backendErr)
	case <-time.After(3 * time.Second):
		t.Fatal("application did not join Sync")
	}
	require.ErrorIs(t, app.Wait(), backendErr)
	require.ErrorIs(t, app.Stop(), backendErr)
	require.NoError(t, pool.Ping(t.Context()))
}
