package postgres_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

// A CONFIG source changes kind in place, as v1.5.4's BulkUpsertConfigSources
// did: same identity, entries kept until the next sync, due at once. Changes
// to or from managed are refused with the source named.
func TestReconcileConfigSourceKindChange(t *testing.T) {
	t.Parallel()
	file := persistence.SourceDefinition{Name: "moving", File: &persistence.FileSpec{Path: "/tmp/registry.json"}, Schedule: "1h"}
	for _, tc := range []struct {
		name    string
		next    persistence.SourceDefinition
		polled  bool // the changed source is one ClaimNext polls
		refused bool
	}{
		{name: "file path to git", polled: true,
			next: persistence.SourceDefinition{Name: "moving", Git: &persistence.GitSpec{Repository: "https://example.org/r.git"}, Schedule: "1h"}},
		{name: "file path to api", polled: true,
			next: persistence.SourceDefinition{Name: "moving", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"}},
		{name: "file path to url", polled: true,
			next: persistence.SourceDefinition{Name: "moving", File: &persistence.FileSpec{URL: "https://example.org/r.json"}, Schedule: "1h"}},
		{name: "file path to kubernetes",
			next: persistence.SourceDefinition{Name: "moving", Kubernetes: &persistence.KubernetesSpec{Namespaces: []string{"default"}}}},
		{name: "file path to managed", refused: true,
			next: persistence.SourceDefinition{Name: "moving", Managed: &persistence.ManagedSpec{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d, pool := newBackend(t)
			jobs, err := postgres.NewJobs(pool, 65536)
			require.NoError(t, err)
			entries, err := postgres.NewEntries(t.Context(), pool, 65536)
			require.NoError(t, err)
			view := []persistence.ViewDefinition{{Name: "main", Sources: []string{"moving"}}}
			require.NoError(t, d.Reconcile(t.Context(), []persistence.SourceDefinition{file}, view))
			job, ok, err := jobs.ClaimNext(t.Context(), time.Minute)
			require.NoError(t, err)
			require.True(t, ok)
			snapshot := model.Snapshot{}
			snapshot.Data.Servers = []model.Server{{
				Schema: "https://example.org/server.schema.json", Name: "com.example/kept", Version: "1", Description: "kept",
			}}
			require.NoError(t, jobs.CommitSnapshot(t.Context(), job, persistence.SyncResult{Hash: "H", FilterHash: "F"}, snapshot))
			before, err := d.GetSource(t.Context(), "moving")
			require.NoError(t, err)

			err = d.Reconcile(t.Context(), []persistence.SourceDefinition{tc.next}, view)
			if tc.refused {
				require.ErrorIs(t, err, persistence.ErrKindChange)
				require.ErrorContains(t, err, `"moving"`)
				got, err := d.GetSource(t.Context(), "moving")
				require.NoError(t, err)
				require.Equal(t, before, got, "a refused change leaves the source alone")
				return
			}
			require.NoError(t, err)
			got, err := d.GetSource(t.Context(), "moving")
			require.NoError(t, err)
			require.Equal(t, before.ID, got.ID, "the source keeps its identity")
			gotKind, _ := got.Kind()
			wantKind, _ := tc.next.Kind()
			require.Equal(t, wantKind, gotKind)
			page, err := entries.ListEntries(t.Context(), persistence.ListOptions{SourceID: got.ID, Kind: persistence.ServerKind, Limit: 10})
			require.NoError(t, err)
			require.Len(t, page.Entries, 1, "entries stay until the next sync replaces them")
			status, err := jobs.Status(t.Context(), "moving")
			require.NoError(t, err)
			require.False(t, status.BaselineValid, "the old baseline no longer applies")
			job, ok, err = jobs.ClaimNext(t.Context(), time.Minute)
			require.NoError(t, err)
			require.Equal(t, tc.polled, ok, "a polled source is due at once")
			if ok {
				require.Equal(t, got.ID, job.Source.ID)
			} else {
				_, err = jobs.Acquire(t.Context(), "moving", time.Minute)
				require.NoError(t, err, "a watched source can be leased under its new kind")
			}
		})
	}
}

// Reconcile errors name the source or registry they concern.
func TestReconcileErrorsNameDefinitions(t *testing.T) {
	t.Parallel()
	file := func(name string) persistence.SourceDefinition {
		return persistence.SourceDefinition{Name: name, File: &persistence.FileSpec{Path: "/tmp/" + name}, Schedule: "1h"}
	}
	for _, tc := range []struct {
		name    string
		setup   func(t *testing.T, d *postgres.Definitions)
		sources []persistence.SourceDefinition
		views   []persistence.ViewDefinition
		want    error
		names   []string
	}{
		{
			name: "config source named like an API source",
			setup: func(t *testing.T, d *postgres.Definitions) {
				t.Helper()
				_, err := d.CreateSource(t.Context(), file("taken"))
				require.NoError(t, err)
			},
			sources: []persistence.SourceDefinition{file("taken")},
			want:    persistence.ErrConflict, names: []string{`"taken"`},
		},
		{
			name: "config registry named like an API registry",
			setup: func(t *testing.T, d *postgres.Definitions) {
				t.Helper()
				_, err := d.CreateSource(t.Context(), file("api-src"))
				require.NoError(t, err)
				_, err = d.CreateView(t.Context(), persistence.ViewDefinition{Name: "shared", Sources: []string{"api-src"}})
				require.NoError(t, err)
			},
			sources: []persistence.SourceDefinition{file("cfg")},
			views:   []persistence.ViewDefinition{{Name: "shared", Sources: []string{"cfg"}}},
			want:    persistence.ErrConflict, names: []string{`"shared"`},
		},
		{
			name: "dropped config source still used by an API registry",
			setup: func(t *testing.T, d *postgres.Definitions) {
				t.Helper()
				require.NoError(t, d.Reconcile(t.Context(), []persistence.SourceDefinition{file("dropped")}, nil))
				_, err := d.CreateView(t.Context(), persistence.ViewDefinition{Name: "api-view", Sources: []string{"dropped"}})
				require.NoError(t, err)
			},
			want: persistence.ErrInUse, names: []string{`"dropped"`, `"api-view"`},
		},
		{
			name: "config managed source beside an API managed source",
			setup: func(t *testing.T, d *postgres.Definitions) {
				t.Helper()
				_, err := d.CreateSource(t.Context(), persistence.SourceDefinition{Name: "api-managed", Managed: &persistence.ManagedSpec{}})
				require.NoError(t, err)
			},
			sources: []persistence.SourceDefinition{{Name: "cfg-managed", Managed: &persistence.ManagedSpec{}}},
			want:    persistence.ErrConflict, names: []string{`"cfg-managed"`, `"api-managed"`},
		},
		{
			name:    "duplicate config source",
			sources: []persistence.SourceDefinition{file("twice"), file("twice")},
			want:    persistence.ErrInvalid, names: []string{`"twice"`},
		},
		{
			name:    "config registry referencing an unknown source",
			sources: []persistence.SourceDefinition{file("known")},
			views:   []persistence.ViewDefinition{{Name: "cfg-view", Sources: []string{"unknown"}}},
			want:    persistence.ErrInvalid, names: []string{`"cfg-view"`, `"unknown"`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d, _ := newBackend(t)
			if tc.setup != nil {
				tc.setup(t, d)
			}
			err := d.Reconcile(t.Context(), tc.sources, tc.views)
			require.ErrorIs(t, err, tc.want)
			for _, name := range tc.names {
				require.ErrorContains(t, err, name)
			}
		})
	}
}
