package storage_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/internal/app/storage"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/db"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	registrypg "github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

//nolint:paralleltest // PGPASSFILE is process-global fixture configuration.
func TestCatalogOwnedPoolPgpassResolution(t *testing.T) {
	conn, cleanup := database.SetupTestDBContainer(t, t.Context())
	t.Cleanup(cleanup)
	require.NoError(t, database.MigrateUp(t.Context(), conn))
	pg := conn.Config()
	require.NotEmpty(t, pg.Password, "fixture requires a password")
	path := filepath.Join(t.TempDir(), ".pgpass")
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf("%s:%d:%s:%s:%s\n", pg.Host, pg.Port, pg.Database, pg.User, pg.Password)), 0600))
	t.Setenv("PGPASSFILE", path)
	cfg := &config.Config{Database: &config.DatabaseConfig{Host: pg.Host, Port: int(pg.Port), User: pg.User, Database: pg.Database, SSLMode: "disable"}}
	store, err := (storage.CatalogPostgres{}).Open(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(store.Close)
	// Read-only preflight forces authentication on the newly owned pool.
	_, err = store.Preflight(t.Context(), nil, nil, "")
	require.NoError(t, err)
}

func TestCatalogEntriesRepairFailureBeforeServing(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDB(t)
	pool, err := pgxpool.New(t.Context(), conn.Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	blocker, err := pool.Begin(t.Context())
	require.NoError(t, err)
	defer blocker.Rollback(context.Background())
	_, err = blocker.Exec(t.Context(), `LOCK TABLE source IN SHARE ROW EXCLUSIVE MODE`)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	entries, err := registrypg.NewEntries(ctx, pool, 65536)
	require.Error(t, err)
	require.Nil(t, entries)
	require.NoError(t, blocker.Commit(t.Context()))
	require.NoError(t, pool.Ping(t.Context()), "borrowed pool remains usable after repair failure")
}

func TestCatalogHistoricalCompatibilityBeforeConsumption(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, kind, spec, filter, schedule, origin, watch, diagnostic string }{
		{"managed-schedule", "managed", "{}", "null", "1h", "CONFIG", "", "schedule/filter"},
		{"kube-filter", "kubernetes", "{}", "{}", "0s", "CONFIG", "", "schedule/filter"},
		{"inline-filter", "file", `{"data":"historical"}`, `{"names":{"include":["private/*"]}}`, "0s", "API", "", "schedule/filter"},
		{"Uppercase", "managed", "{}", "null", "0s", "CONFIG", "", "name"},
		{"unknown-field", "api", `{"endpoint":"https://example.org","extra":"preserve"}`, "null", "1h", "API", "", "unsupported fields"},
		{"disabled-ingestion", "api", `{"endpoint":"https://example.org"}`, "null", "1h", "API", "", "syncable flag"},
		{"implicit-scope", "kubernetes", "{}", "null", "0s", "API", "one,two", "materialize"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			conn, _ := database.SetupTestDB(t)
			_, err := conn.Exec(t.Context(), `INSERT INTO source(name,source_type,source_config,filter_config,sync_schedule,syncable,creation_type) VALUES($1,$2,$3,$4,$5::interval,false,$6)`, tc.name, tc.kind, tc.spec, tc.filter, tc.schedule, tc.origin)
			require.NoError(t, err)
			pool, err := pgxpool.New(t.Context(), conn.Config().ConnString())
			require.NoError(t, err)
			t.Cleanup(pool.Close)
			store, err := (storage.CatalogPostgres{BorrowedPool: pool}).Open(t.Context(), &config.Config{Database: &config.DatabaseConfig{}})
			require.NoError(t, err)
			defer store.Close()
			before := conn.PgConn().PID()
			// Even a CONFIG source absent from the final set must be classified, not
			// silently pruned. API/unlinked sources are equally part of preparation.
			_, err = store.Preflight(t.Context(), nil, nil, tc.watch)
			require.ErrorContains(t, err, tc.diagnostic)
			var name string
			require.NoError(t, conn.QueryRow(t.Context(), `SELECT name FROM source`).Scan(&name))
			require.Equal(t, tc.name, name)
			var consumed bool
			require.NoError(t, conn.QueryRow(t.Context(), `SELECT consumed_at IS NOT NULL FROM exposure_acknowledgement`).Scan(&consumed))
			require.False(t, consumed)
			require.Equal(t, before, conn.PgConn().PID())
			require.NoError(t, pool.Ping(t.Context()))
		})
	}
}
func TestCatalogNamespacePrecedenceCanonicalAndStored(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		explicit []string
		global   string
		want     []string
		invalid  bool
	}{
		{[]string{"two", "one", "one"}, "other", []string{"one", "two"}, false},
		{nil, " two,one,one ", []string{"one", "two"}, false},
		{nil, "", nil, false}, {nil, "one,,two", nil, true}, {[]string{"INVALID"}, "one", nil, true},
	} {
		got, err := storage.EffectiveNamespaces(tc.explicit, tc.global)
		if tc.invalid {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		}
	}
	conn, _ := database.SetupTestDB(t)
	pool, err := pgxpool.New(t.Context(), conn.Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	store, err := (storage.CatalogPostgres{BorrowedPool: pool}).Open(t.Context(), &config.Config{Database: &config.DatabaseConfig{}})
	require.NoError(t, err)
	defer store.Close()
	namespaces, err := storage.EffectiveNamespaces(nil, "two,one")
	require.NoError(t, err)
	desired := []persistence.SourceDefinition{{Name: "operator", Kubernetes: &persistence.KubernetesSpec{Namespaces: namespaces}}}
	_, err = store.Preflight(t.Context(), desired, nil, "two,one")
	require.NoError(t, err)
	revision := "sha256:" + strings.Repeat("a", 64)
	inventory, err := db.InventoryExposure(t.Context(), conn, revision)
	require.NoError(t, err)
	_, err = db.AcknowledgeExposure(t.Context(), conn, revision, inventory.Digest, false, true, true)
	require.NoError(t, err)
	require.NoError(t, store.Consume(t.Context(), revision))
	components, err := store.Initialize(t.Context(), desired, nil)
	require.NoError(t, err)
	job, err := components.Jobs.Acquire(t.Context(), "operator", time.Second)
	require.NoError(t, err)
	require.Equal(t, []string{"one", "two"}, job.Source.Kubernetes.Namespaces)
}
