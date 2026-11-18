package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/database/dbtest"
	dbcore "github.com/stacklok/toolhive-registry-server/internal/db"
)

func poolAt(t *testing.T, version uint) *pgxpool.Pool {
	t.Helper()
	conn, cleanup := dbtest.SetupTestDBAt(t, version)
	t.Cleanup(cleanup)
	pool, err := pgxpool.New(context.Background(), conn.Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func TestFreshDatabaseRecordsFirstMode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := poolAt(t, database.LatestSchemaVersion)

	require.NoError(t, dbcore.EnsureAuthorizationMode(ctx, pool, "cedar"), "a fresh database takes the configured mode")
	require.NoError(t, dbcore.EnsureAuthorizationMode(ctx, pool, "cedar"), "restarting in the same mode is fine")
	err := dbcore.EnsureAuthorizationMode(ctx, pool, "claims")
	require.ErrorIs(t, err, dbcore.ErrAuthorizationModeMismatch)
	assert.Contains(t, err.Error(), "this database uses cedar mode but the configuration selects claims mode")
}

func TestUpgradedCatalogIsClaimsMode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := poolAt(t, 28)
	_, err := pool.Exec(ctx, `INSERT INTO source (name, creation_type, source_type, syncable)
		VALUES ('existing', 'CONFIG', 'managed', false)`)
	require.NoError(t, err)
	require.NoError(t, database.MigrateUp(ctx, migrateConn(t, pool)))

	err = dbcore.EnsureAuthorizationMode(ctx, pool, "cedar")
	require.ErrorIs(t, err, dbcore.ErrAuthorizationModeMismatch, "a pre-Cedar catalog cannot start in Cedar mode")
	require.NoError(t, dbcore.EnsureAuthorizationMode(ctx, pool, "claims"))
}

func TestEmptyDatabaseUpgradeLeavesModeOpen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := poolAt(t, 28)
	require.NoError(t, database.MigrateUp(ctx, migrateConn(t, pool)))
	require.NoError(t, dbcore.EnsureAuthorizationMode(ctx, pool, "cedar"), "no catalog yet, so any mode may start")
}

func TestAuthorizationModeDowngrade(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := poolAt(t, database.LatestSchemaVersion)
	require.NoError(t, dbcore.EnsureAuthorizationMode(ctx, pool, "claims"))
	require.NoError(t, database.MigrateDown(ctx, migrateConn(t, pool), 1))
	version, dirty := versionAt(t, pool)
	assert.Equal(t, uint(28), version)
	assert.False(t, dirty)
	var exists bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT to_regclass('authorization_mode') IS NOT NULL`).Scan(&exists))
	assert.False(t, exists)
}
