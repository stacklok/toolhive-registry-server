package postgres_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
)

func TestReusableCoordinatorDefinitionFence(t *testing.T) {
	t.Parallel()
	definitions, pool := newBackend(t)
	entries, err := postgres.NewEntries(t.Context(), pool, 65536)
	require.NoError(t, err)
	jobs, err := postgres.NewJobs(pool, 65536)
	require.NoError(t, err)
	var sources []persistence.SourceDefinition
	original := model.Snapshot{}
	original.Data.Servers = []model.Server{{Schema: "https://example.org/server.schema.json", Name: "com.example/server", Version: "1", Description: "retained"}}
	for _, name := range []string{"operator", "unrelated"} {
		source, err := definitions.CreateSource(t.Context(), persistence.SourceDefinition{Name: name, Kubernetes: &persistence.KubernetesSpec{Namespaces: []string{"old"}}})
		require.NoError(t, err)
		require.NoError(t, entries.ReplaceSnapshot(t.Context(), source, original))
		sources = append(sources, source)
	}
	fetcher := mocks.NewMockFetcher(gomock.NewController(t))
	fetcher.EXPECT().Fetch(gomock.Any(), sources[0]).DoAndReturn(func(ctx context.Context, captured persistence.SourceDefinition) (ingest.Result, error) {
		captured.Kubernetes = &persistence.KubernetesSpec{Namespaces: []string{"new"}}
		_, err := definitions.UpdateSource(ctx, captured.Name, captured)
		require.NoError(t, err)
		return ingest.Result{Snapshot: model.Snapshot{}, Hash: "empty-old-scope"}, nil
	})
	coordinator, err := reconcile.New(jobs, fetcher, reconcile.Options{PollInterval: time.Second, LeaseDuration: time.Minute})
	require.NoError(t, err)
	require.ErrorIs(t, coordinator.Sync(t.Context(), sources[0].Name), persistence.ErrStale)
	for _, source := range sources {
		page, err := entries.ListEntries(t.Context(), persistence.ListOptions{SourceID: source.ID, Kind: persistence.ServerKind, Limit: 10})
		require.NoError(t, err)
		require.Len(t, page.Entries, 1)
		require.Equal(t, "retained", page.Entries[0].Server.Description)
	}
}

func TestReusableCoordinatorDeletedDuringFetch(t *testing.T) {
	t.Parallel()
	for _, recreate := range []bool{false, true} {
		t.Run(fmt.Sprint(recreate), func(t *testing.T) {
			t.Parallel()
			definitions, pool := newBackend(t)
			entries, err := postgres.NewEntries(t.Context(), pool, 65536)
			require.NoError(t, err)
			jobs, err := postgres.NewJobs(pool, 65536)
			require.NoError(t, err)
			source, err := definitions.CreateSource(t.Context(), persistence.SourceDefinition{Name: "operator", Kubernetes: &persistence.KubernetesSpec{}})
			require.NoError(t, err)
			var successor persistence.SourceDefinition
			fetcher := mocks.NewMockFetcher(gomock.NewController(t))
			fetcher.EXPECT().Fetch(gomock.Any(), source).DoAndReturn(func(ctx context.Context, captured persistence.SourceDefinition) (ingest.Result, error) {
				require.NoError(t, definitions.DeleteSource(ctx, captured.Name))
				if recreate {
					successor, err = definitions.CreateSource(ctx, persistence.SourceDefinition{Name: captured.Name, Kubernetes: &persistence.KubernetesSpec{}})
					require.NoError(t, err)
					retained := model.Snapshot{}
					retained.Data.Servers = []model.Server{{Schema: "https://example.org/server.schema.json", Name: "com.example/server", Version: "1", Description: "successor data"}}
					require.NoError(t, entries.ReplaceSnapshot(ctx, successor, retained))
				}
				return ingest.Result{Snapshot: model.Snapshot{}, Hash: "old-empty-snapshot"}, nil
			})
			coordinator, err := reconcile.New(jobs, fetcher, reconcile.Options{PollInterval: time.Second, LeaseDuration: time.Minute})
			require.NoError(t, err)
			require.ErrorIs(t, coordinator.Sync(t.Context(), source.Name), persistence.ErrStale)
			status, err := jobs.Status(t.Context(), source.Name)
			if recreate {
				require.NoError(t, err)
				require.Equal(t, successor.ID, status.SourceID)
				require.Zero(t, status.Attempts)
				require.Empty(t, status.LastHash)
				page, err := entries.ListEntries(t.Context(), persistence.ListOptions{SourceID: successor.ID, Kind: persistence.ServerKind, Limit: 10})
				require.NoError(t, err)
				require.Len(t, page.Entries, 1)
				require.Equal(t, "successor data", page.Entries[0].Server.Description)
			} else {
				require.ErrorIs(t, err, persistence.ErrNotFound)
			}
		})
	}
}

func TestReusableCoordinatorBaseline(t *testing.T) {
	t.Parallel()
	definitions, pool := newBackend(t)
	entries, err := postgres.NewEntries(t.Context(), pool, 65536)
	require.NoError(t, err)
	jobs, err := postgres.NewJobs(pool, 65536)
	require.NoError(t, err)
	source, err := definitions.CreateSource(t.Context(), persistence.SourceDefinition{Name: "inline", File: &persistence.FileSpec{Data: `{"version":"1.0.0","meta":{"last_updated":"2025-01-15T10:30:00Z"},"data":{"servers":[{"$schema":"https://example.org/server.schema.json","name":"com.example/server","version":"1","description":"Server"}],"skills":[{"namespace":"io.example","name":"skill","description":"Skill","version":"1"}],"plugins":[{"namespace":"io.example","name":"plugin","description":"Plugin","version":"1"}]}}`}})
	require.NoError(t, err)
	c, err := reconcile.New(jobs, ingest.New(ingest.Options{}), reconcile.Options{PollInterval: time.Second, LeaseDuration: time.Minute})
	require.NoError(t, err)
	for _, step := range []string{"first", "unchanged", "direct invalidation"} {
		if step == "direct invalidation" {
			require.NoError(t, entries.ReplaceSnapshot(t.Context(), source, model.Snapshot{}))
		}
		require.NoError(t, c.Sync(t.Context(), source.Name))
		status, err := jobs.Status(t.Context(), source.Name)
		require.NoError(t, err)
		require.True(t, status.BaselineValid)
		require.Equal(t, int64(1), status.ServerCount)
		require.Equal(t, int64(1), status.SkillCount)
		require.Equal(t, int64(1), status.PluginCount)
		for _, kind := range []persistence.EntryKind{persistence.ServerKind, persistence.SkillKind, persistence.PluginKind} {
			page, err := entries.ListEntries(t.Context(), persistence.ListOptions{SourceID: source.ID, Kind: kind, Limit: 10})
			require.NoError(t, err)
			require.Len(t, page.Entries, 1)
		}
	}
}
