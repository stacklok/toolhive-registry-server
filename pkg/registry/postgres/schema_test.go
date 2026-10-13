package postgres_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

func TestSchemaCompatibilityIsHTTPBackendMaintenance(t *testing.T) {
	t.Parallel()
	defs, pool := newBackend(t)
	entries, err := postgres.NewEntries(t.Context(), pool, 65536)
	require.NoError(t, err)
	jobs, err := postgres.NewJobs(pool, 65536)
	require.NoError(t, err)
	diagnostics, err := catalog.NewDiagnostics(entries, jobs)
	require.NoError(t, err)
	router, err := httpapi.AdminRouter(httpapi.AdminOperations{
		Sources: defs, Views: defs, Publisher: entries, Diagnostics: diagnostics,
	}, httpapi.AdminOptions{})
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `UPDATE schema_migrations SET version=29`)
	require.NoError(t, err)
	_, err = defs.CreateSource(t.Context(), persistence.SourceDefinition{Name: "item", Managed: &persistence.ManagedSpec{}})
	require.ErrorIs(t, err, persistence.ErrUnavailable)
	require.NotErrorIs(t, err, persistence.ErrConflict)
	require.ErrorContains(t, err, "clean schema 30 required")
	response := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/sources/item", strings.NewReader(`{"managed":{}}`))
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.NotContains(t, response.Body.String(), "schema")
	require.NotContains(t, response.Body.String(), "upgrade")
}

func TestSchemaGatePreservesCancellation(t *testing.T) {
	t.Parallel()
	defs, pool := newBackend(t)
	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) }()
	_, err = tx.Exec(t.Context(), `LOCK TABLE schema_migrations IN ACCESS EXCLUSIVE MODE`)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := defs.CreateSource(ctx, persistence.SourceDefinition{Name: "cancelled", Managed: &persistence.ManagedSpec{}})
		finished <- err
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
 WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock')`).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-finished, context.Canceled)
	var sources int
	require.NoError(t, tx.QueryRow(t.Context(), `SELECT count(*) FROM source`).Scan(&sources))
	require.Zero(t, sources)
}

func TestHistoricalSchemaRejectsEveryPublicWrite(t *testing.T) {
	t.Parallel()
	for _, claims := range []string{"null", `"SQL NULL"`, `{}`, `{"team":"private"}`} {
		t.Run(claims, func(t *testing.T) {
			t.Parallel()
			defs, pool := newBackendAt(t, 29)
			ctx := t.Context()
			var value any = claims
			if claims == `"SQL NULL"` {
				value = nil
			}
			_, err := pool.Exec(ctx, `INSERT INTO source(name,source_type,source_config,creation_type,syncable,claims)
 VALUES('history','managed','{}','API',false,$1::jsonb)`, value)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, `INSERT INTO registry(name,creation_type) VALUES('history','API')`)
			require.NoError(t, err)
			var before, after string
			require.NoError(t, pool.QueryRow(ctx, `SELECT row_to_json(s)::text FROM source s`).Scan(&before))
			_, err = postgres.NewEntries(ctx, pool, 65536)
			require.ErrorIs(t, err, persistence.ErrUnavailable, "constructor must not repair historical latest pointers")
			jobs, err := postgres.NewJobs(pool, 65536)
			require.NoError(t, err)
			_, _, err = jobs.ClaimNext(ctx, time.Minute)
			require.ErrorIs(t, err, persistence.ErrUnavailable)
			_, err = jobs.Acquire(ctx, "history", time.Minute)
			require.ErrorIs(t, err, persistence.ErrUnavailable)
			_, err = defs.CreateSource(ctx, persistence.SourceDefinition{Name: "new", File: &persistence.FileSpec{Data: "{}"}})
			require.ErrorIs(t, err, persistence.ErrUnavailable)
			_, err = defs.UpdateSource(ctx, "history", persistence.SourceDefinition{Name: "history", Managed: &persistence.ManagedSpec{}})
			require.ErrorIs(t, err, persistence.ErrUnavailable)
			require.ErrorIs(t, defs.DeleteSource(ctx, "history"), persistence.ErrUnavailable)
			_, err = defs.CreateView(ctx, persistence.ViewDefinition{Name: "new", Sources: []string{"history"}})
			require.ErrorIs(t, err, persistence.ErrUnavailable)
			_, err = defs.UpdateView(ctx, "history", persistence.ViewDefinition{Name: "history", Sources: []string{"history"}})
			require.ErrorIs(t, err, persistence.ErrUnavailable)
			require.ErrorIs(t, defs.DeleteView(ctx, "history"), persistence.ErrUnavailable)
			require.ErrorIs(t, defs.Reconcile(ctx, nil, nil), persistence.ErrUnavailable)
			require.NoError(t, pool.QueryRow(ctx, `SELECT row_to_json(s)::text FROM source s`).Scan(&after))
			require.Equal(t, before, after)
		})
	}
}

func TestEntriesDoNotCacheSchemaCompatibility(t *testing.T) {
	t.Parallel()
	for _, schema := range []struct {
		version int
		dirty   bool
	}{{29, false}, {30, true}, {31, false}} {
		t.Run(fmt.Sprintf("schema-%d-dirty-%t", schema.version, schema.dirty), func(t *testing.T) {
			t.Parallel()
			defs, pool := newBackend(t)
			ctx := t.Context()
			entries, err := postgres.NewEntries(ctx, pool, 65536)
			require.NoError(t, err)
			source, err := defs.CreateSource(ctx, persistence.SourceDefinition{Name: "external", File: &persistence.FileSpec{Data: "{}"}})
			require.NoError(t, err)
			_, err = defs.CreateSource(ctx, persistence.SourceDefinition{Name: "managed", Managed: &persistence.ManagedSpec{}})
			require.NoError(t, err)
			_, err = pool.Exec(ctx, `UPDATE schema_migrations SET version=$1,dirty=$2`, schema.version, schema.dirty)
			require.NoError(t, err)
			require.ErrorIs(t, entries.ReplaceSnapshot(ctx, source, model.Snapshot{}), persistence.ErrUnavailable)
			_, err = entries.Publish(ctx, persistence.Entry{Kind: persistence.SkillKind, Name: "item", Version: "1",
				Skill: &model.Skill{Namespace: "com.example", Name: "item", Version: "1", Description: "retained"}})
			require.ErrorIs(t, err, persistence.ErrUnavailable)
			require.ErrorIs(t, entries.DeleteManaged(ctx, persistence.SkillKind, "item", "1"), persistence.ErrUnavailable)
			_, err = postgres.NewEntries(ctx, pool, 65536)
			require.ErrorIs(t, err, persistence.ErrUnavailable)
		})
	}
}
