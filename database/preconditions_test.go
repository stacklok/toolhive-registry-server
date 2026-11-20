package database_test

import (
	"errors"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/database/dbtest"
)

// schemaState is what a refused migration must leave unchanged.
type schemaState struct {
	Version uint
	Dirty   bool
}

func readSchemaState(t *testing.T, conn *pgx.Conn) schemaState {
	t.Helper()
	version, dirty, err := database.GetVersion(conn.Config().ConnString())
	require.NoError(t, err)
	return schemaState{Version: version, Dirty: dirty}
}

func exists(t *testing.T, conn *pgx.Conn, query string) bool {
	t.Helper()
	var ok bool
	require.NoError(t, conn.QueryRow(t.Context(), query).Scan(&ok))
	return ok
}

// Migration 24 adds a unique index for the single managed source. A database
// with two managed sources is refused before anything runs, so it stays at
// its old version, clean, and can still be repaired and upgraded.
func TestMigrateUpRefusesSecondManagedSource(t *testing.T) {
	t.Parallel()
	const singleManagedIndex = `SELECT to_regclass('public.source_single_managed_idx') IS NOT NULL`
	for _, tc := range []struct {
		name    string
		managed []string
		refused bool
	}{
		{name: "no managed source", managed: nil},
		{name: "one managed source", managed: []string{"published"}},
		{name: "two managed sources", managed: []string{"published", "legacy"}, refused: true},
		{name: "three managed sources", managed: []string{"a-managed", "b-managed", "c-managed"}, refused: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			conn, _ := dbtest.SetupTestDBAt(t, 23)
			_, err := conn.Exec(t.Context(), `INSERT INTO source(name, source_type, creation_type) VALUES ('synced', 'file', 'CONFIG')`)
			require.NoError(t, err)
			for _, name := range tc.managed {
				_, err := conn.Exec(t.Context(),
					`INSERT INTO source(name, source_type, creation_type, syncable) VALUES ($1, 'managed', 'API', false)`, name)
				require.NoError(t, err)
			}

			err = database.MigrateUp(t.Context(), conn)
			if !tc.refused {
				require.NoError(t, err)
				require.Equal(t, schemaState{Version: database.LatestSchemaVersion}, readSchemaState(t, conn))
				return
			}
			require.ErrorIs(t, err, database.ErrPrecondition)
			for _, name := range tc.managed {
				require.ErrorContains(t, err, name)
			}
			require.ErrorContains(t, err, "docs/database.md")
			require.Equal(t, schemaState{Version: 23}, readSchemaState(t, conn), "a refused upgrade leaves the database untouched")
			require.False(t, exists(t, conn, singleManagedIndex))

			// The documented repair: remove the extra managed sources, then upgrade.
			_, err = conn.Exec(t.Context(), `DELETE FROM source WHERE source_type = 'managed' AND name <> $1`, tc.managed[0])
			require.NoError(t, err)
			require.NoError(t, database.MigrateUp(t.Context(), conn))
			require.Equal(t, schemaState{Version: database.LatestSchemaVersion}, readSchemaState(t, conn))
			require.True(t, exists(t, conn, singleManagedIndex))
		})
	}
}

// The same check reads the pre-split schemas, where managed sources were
// registry rows typed MANAGED.
func TestMigrateUpRefusesSecondManagedSourceOnOldSchemas(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		version uint
		insert  string
	}{
		{name: "registry table with source_type", version: 14,
			insert: `INSERT INTO registry(name, reg_type, source_type) VALUES ('m1', 'MANAGED', NULL), ('m2', 'REMOTE', 'managed')`},
		{name: "registry table before source_type", version: 7,
			insert: `INSERT INTO registry(name, reg_type) VALUES ('m1', 'MANAGED'), ('m2', 'MANAGED')`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			conn, _ := dbtest.SetupTestDBAt(t, tc.version)
			_, err := conn.Exec(t.Context(), tc.insert)
			require.NoError(t, err)
			err = database.MigrateUp(t.Context(), conn)
			require.ErrorIs(t, err, database.ErrPrecondition)
			require.ErrorContains(t, err, "m1, m2")
			require.Equal(t, schemaState{Version: tc.version}, readSchemaState(t, conn))
		})
	}
}

// Reverting migration 26 while a sync lease is live is refused before any
// later migration is reverted, so nothing is left dirty at the wrong version.
func TestMigrateDownRefusesLiveSyncLease(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		lease   string // interval added to now for lease_expires_at; empty for no lease
		steps   int
		refused bool
		want    uint
	}{
		{name: "live lease, crossing 26", lease: "1 hour", steps: 2, refused: true},
		{name: "live lease, crossing 26 and more", lease: "1 hour", steps: 5, refused: true},
		{name: "live lease, not crossing 26", lease: "1 hour", steps: 1, want: 26},
		{name: "expired lease, crossing 26", lease: "-1 hour", steps: 2, want: 25},
		{name: "no lease, crossing 26", steps: 2, want: 25},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			conn, _ := dbtest.SetupTestDB(t)
			// The cases count steps from schema 27; later branches add migrations.
			if extra := int(database.LatestSchemaVersion) - 27; extra > 0 {
				require.NoError(t, database.MigrateDown(t.Context(), conn, extra))
			}
			var sourceID string
			require.NoError(t, conn.QueryRow(t.Context(), `INSERT INTO source(name, source_type, creation_type, source_config, sync_schedule)
VALUES ('leased', 'file', 'CONFIG', '{"path":"/tmp/x"}', interval '1 hour') RETURNING id::text`).Scan(&sourceID))
			if tc.lease != "" {
				_, err := conn.Exec(t.Context(), `INSERT INTO registry_sync(source_id, sync_status, started_at, lease_id,
 lease_expires_at, lease_generation, observed_generation)
VALUES ($1, 'IN_PROGRESS', clock_timestamp(), gen_random_uuid(), clock_timestamp() + $2::interval, 1, 1)`, sourceID, tc.lease)
				require.NoError(t, err)
			}

			err := database.MigrateDown(t.Context(), conn, tc.steps)
			if !tc.refused {
				require.NoError(t, err)
				require.Equal(t, schemaState{Version: tc.want}, readSchemaState(t, conn))
				return
			}
			require.ErrorIs(t, err, database.ErrPrecondition)
			require.ErrorContains(t, err, "leased")
			require.Equal(t, schemaState{Version: 27}, readSchemaState(t, conn),
				"a refused downgrade reverts nothing")
			// The lease columns (migration 26) and the lease itself are still there.
			require.True(t, exists(t, conn, `SELECT EXISTS (SELECT 1 FROM registry_sync
 WHERE lease_id IS NOT NULL AND lease_expires_at > clock_timestamp())`), "the lease is untouched")
		})
	}
}

// A dirty schema is refused with a pointer to the recovery procedure, and
// Force records the version so migrations can run again.
func TestMigrateDirtyAndForce(t *testing.T) {
	t.Parallel()
	conn, _ := dbtest.SetupTestDB(t)
	_, err := conn.Exec(t.Context(), `UPDATE schema_migrations SET dirty = true`)
	require.NoError(t, err)

	err = database.MigrateUp(t.Context(), conn)
	var dirty migrate.ErrDirty
	require.True(t, errors.As(err, &dirty), "got %v", err)
	require.ErrorContains(t, err, "migrate force")
	require.ErrorIs(t, database.MigrateDown(t.Context(), conn, 1), migrate.ErrDirty{Version: int(database.LatestSchemaVersion)})

	require.NoError(t, database.Force(conn.Config().ConnString(), int(database.LatestSchemaVersion)))
	require.Equal(t, schemaState{Version: database.LatestSchemaVersion}, readSchemaState(t, conn))
	require.NoError(t, database.MigrateUp(t.Context(), conn))
	require.Error(t, database.Force(conn.Config().ConnString(), -2))
}

func TestMigrateDownRefusesCedarMode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		mode    string
		refused bool
	}{
		{name: "cedar database", mode: "cedar", refused: true},
		{name: "claims database", mode: "claims"},
		{name: "no recorded mode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			conn, _ := dbtest.SetupTestDB(t)
			if tc.mode != "" {
				_, err := conn.Exec(t.Context(), `INSERT INTO authorization_mode (mode) VALUES ($1)`, tc.mode)
				require.NoError(t, err)
			}
			err := database.MigrateDown(t.Context(), conn, 1)
			if !tc.refused {
				require.NoError(t, err)
				require.Equal(t, schemaState{Version: 28}, readSchemaState(t, conn))
				return
			}
			require.ErrorIs(t, err, database.ErrPrecondition)
			require.ErrorContains(t, err, "Cedar")
			require.Equal(t, schemaState{Version: 29}, readSchemaState(t, conn), "a refused downgrade reverts nothing")
			require.True(t, exists(t, conn, `SELECT EXISTS (SELECT 1 FROM authorization_mode WHERE mode = 'cedar')`))
		})
	}
}
