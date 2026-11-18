package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/database/dbtest"
	dbcore "github.com/stacklok/toolhive-registry-server/internal/db"
)

func repairTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	db, cleanup := dbtest.SetupTestDBAt(t, 26)
	t.Cleanup(cleanup)
	pool, err := pgxpool.New(context.Background(), db.Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func repairTestEntry(t *testing.T, pool *pgxpool.Pool, source uuid.UUID, kind, name, version string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var entry, id uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO registry_entry (source_id, entry_type, name, claims)
		VALUES ($1,$2,$3,'{"team":"example"}') RETURNING id`, source, kind, name).Scan(&entry))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO entry_version (entry_id, name, version, title)
		VALUES ($1,$2,$3,'unchanged') RETURNING id`, entry, name, version).Scan(&id))
	return id
}

func repairTestSource(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	var source uuid.UUID
	require.NoError(t, pool.QueryRow(context.Background(), `INSERT INTO source (name, creation_type, source_type, syncable)
		VALUES ('repair-source','API','managed',false) RETURNING id`).Scan(&source))
	return source
}

func waitForRepairLockWait(t *testing.T, pool *pgxpool.Pool, queryPrefix string) {
	t.Helper()
	require.Eventually(t, func() bool {
		var waiting bool
		err := pool.QueryRow(context.Background(), `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE $1
		)`, queryPrefix+"%").Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond, "expected database lock wait for %s", queryPrefix)
}

func TestRepairLegacyKindCollisions(t *testing.T) {
	t.Parallel()
	pool := repairTestPool(t)
	ctx := context.Background()
	source := repairTestSource(t, pool)
	for _, tc := range []struct {
		name    string
		pointed bool
	}{
		{"pointed", true}, {"unpointed", false},
	} {
		old := repairTestEntry(t, pool, source, "MCP", tc.name, "1.0.0")
		var entry uuid.UUID
		require.NoError(t, pool.QueryRow(ctx, `SELECT entry_id FROM entry_version WHERE id=$1`, old).Scan(&entry))
		var newer uuid.UUID
		require.NoError(t, pool.QueryRow(ctx, `INSERT INTO entry_version (entry_id,name,version) VALUES ($1,$2,'v1.0.0') RETURNING id`, entry, tc.name).Scan(&newer))
		_ = repairTestEntry(t, pool, source, "SKILL", tc.name, "9.0.0")
		if tc.pointed {
			_, err := pool.Exec(ctx, `INSERT INTO latest_entry_version (source_id,entry_type,name,version,latest_version_id) VALUES ($1,'MCP',$2,'1.0.0',$3)`, source, tc.name, old)
			require.NoError(t, err)
		}
	}
	_ = repairTestEntry(t, pool, source, "PLUGIN", "unambiguous", "1.0.0")
	for range 2 {
		require.NoError(t, dbcore.ReconcileLatestVersions(ctx, pool))
		var got uuid.UUID
		require.NoError(t, pool.QueryRow(ctx, `SELECT latest_version_id FROM latest_entry_version WHERE source_id=$1 AND entry_type='MCP' AND name='pointed'`, source).Scan(&got))
		var want uuid.UUID
		require.NoError(t, pool.QueryRow(ctx, `SELECT v.id FROM entry_version v JOIN registry_entry e ON e.id=v.entry_id WHERE e.source_id=$1 AND e.entry_type='MCP' AND e.name='pointed' AND v.version='v1.0.0'`, source).Scan(&want))
		require.Equal(t, want, got)
		var storedVersion string
		require.NoError(t, pool.QueryRow(ctx, `SELECT version FROM latest_entry_version WHERE source_id=$1 AND entry_type='MCP' AND name='pointed'`, source).Scan(&storedVersion))
		require.Equal(t, "v1.0.0", storedVersion)
		var unpointedMCP, unpointedSkill uuid.UUID
		require.NoError(t, pool.QueryRow(ctx, `SELECT latest_version_id FROM latest_entry_version WHERE source_id=$1 AND entry_type='MCP' AND name='unpointed'`, source).Scan(&unpointedMCP))
		require.NoError(t, pool.QueryRow(ctx, `SELECT latest_version_id FROM latest_entry_version WHERE source_id=$1 AND entry_type='SKILL' AND name='unpointed'`, source).Scan(&unpointedSkill))
		require.NotEqual(t, unpointedMCP, unpointedSkill)
		require.NoError(t, pool.QueryRow(ctx, `SELECT latest_version_id FROM latest_entry_version WHERE source_id=$1 AND entry_type='PLUGIN' AND name='unambiguous'`, source).Scan(&got))
	}
	_, err := pool.Exec(ctx, `UPDATE latest_entry_version SET version='stale' WHERE source_id=$1 AND entry_type='MCP' AND name='pointed'`, source)
	require.NoError(t, err)
	require.NoError(t, dbcore.ReconcileLatestVersions(ctx, pool))
	var storedVersion string
	require.NoError(t, pool.QueryRow(ctx, `SELECT version FROM latest_entry_version WHERE source_id=$1 AND entry_type='MCP' AND name='pointed'`, source).Scan(&storedVersion))
	require.Equal(t, "v1.0.0", storedVersion)
}

func TestRepairSerializesConcurrentVersionWrites(t *testing.T) {
	t.Parallel()
	pool := repairTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	source := repairTestSource(t, pool)
	old := repairTestEntry(t, pool, source, "MCP", "shared", "1.0.0")
	var entry uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, `SELECT entry_id FROM entry_version WHERE id=$1`, old).Scan(&entry))
	var tied uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO entry_version (entry_id,name,version) VALUES ($1,'shared','v1.0.0') RETURNING id`, entry).Scan(&tied))
	_, err := pool.Exec(ctx, `INSERT INTO latest_entry_version (source_id,entry_type,name,version,latest_version_id) VALUES ($1,'MCP','shared','1.0.0',$2)`, source, old)
	require.NoError(t, err)

	// A publish already in progress owns a write lock; repair must wait for its
	// commit before scanning, rather than overwrite the newly published pointer.
	writer, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = writer.Rollback(context.Background()) }()
	var published uuid.UUID
	require.NoError(t, writer.QueryRow(ctx, `INSERT INTO entry_version (entry_id,name,version) VALUES ($1,'shared','2.0.0') RETURNING id`, entry).Scan(&published))
	_, err = writer.Exec(ctx, `UPDATE latest_entry_version SET latest_version_id=$1,version='2.0.0' WHERE source_id=$2 AND name='shared'`, published, source)
	require.NoError(t, err)
	finished := make(chan error, 1)
	go func() { finished <- dbcore.ReconcileLatestVersions(ctx, pool) }()
	waitForRepairLockWait(t, pool, "LOCK TABLE source, registry_entry")
	select {
	case err := <-finished:
		t.Fatalf("repair returned before writer committed: %v", err)
	default:
	}
	require.NoError(t, writer.Commit(ctx))
	require.NoError(t, <-finished)
	var got uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, `SELECT latest_version_id FROM latest_entry_version WHERE source_id=$1 AND name='shared'`, source).Scan(&got))
	require.Equal(t, published, got)

	// A later deletion must wait for the repair's table lock; when it commits,
	// the latest pointer is its own committed selection, not the stale scan.
	lock, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = lock.Rollback(context.Background()) }()
	_, err = lock.Exec(ctx, `LOCK TABLE source, registry_entry, entry_version, latest_entry_version IN SHARE ROW EXCLUSIVE MODE`)
	require.NoError(t, err)
	require.NoError(t, dbcore.ReconcileLatestVersionsTx(ctx, lock))
	deleted := make(chan error, 1)
	go func() {
		deletion, err := pool.Begin(ctx)
		if err != nil {
			deleted <- err
			return
		}
		defer func() { _ = deletion.Rollback(context.Background()) }()
		if _, err = deletion.Exec(ctx, `UPDATE latest_entry_version SET latest_version_id=$1,version='v1.0.0' WHERE source_id=$2 AND name='shared'`, tied, source); err == nil {
			_, err = deletion.Exec(ctx, `DELETE FROM entry_version WHERE id=$1`, published)
		}
		if err == nil {
			err = deletion.Commit(ctx)
		}
		deleted <- err
	}()
	waitForRepairLockWait(t, pool, "UPDATE latest_entry_version SET latest_version_id")
	require.NoError(t, lock.Commit(ctx))
	require.NoError(t, <-deleted)
	require.NoError(t, pool.QueryRow(ctx, `SELECT latest_version_id FROM latest_entry_version WHERE source_id=$1 AND name='shared'`, source).Scan(&got))
	require.Equal(t, tied, got)
	require.ErrorIs(t, pool.QueryRow(ctx, `SELECT id FROM entry_version WHERE id=$1`, published).Scan(&got), pgx.ErrNoRows)
}

func TestRepairCancellationRollsBack(t *testing.T) {
	t.Parallel()
	pool := repairTestPool(t)
	ctx := context.Background()
	source := repairTestSource(t, pool)
	old := repairTestEntry(t, pool, source, "MCP", "blocked", "1.0.0")
	var entry uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, `SELECT entry_id FROM entry_version WHERE id=$1`, old).Scan(&entry))
	_, err := pool.Exec(ctx, `INSERT INTO entry_version (entry_id,name,version) VALUES ($1,'blocked','v1.0.0')`, entry)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO latest_entry_version (source_id,entry_type,name,version,latest_version_id) VALUES ($1,'MCP','blocked','1.0.0',$2)`, source, old)
	require.NoError(t, err)
	blocker, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback(context.Background()) }()
	_, err = blocker.Exec(ctx, `LOCK TABLE source IN SHARE ROW EXCLUSIVE MODE`)
	require.NoError(t, err)
	active, cancel := context.WithCancel(ctx)
	finished := make(chan error, 1)
	go func() { finished <- dbcore.ReconcileLatestVersions(active, pool) }()
	waitForRepairLockWait(t, pool, "LOCK TABLE source, registry_entry")
	cancel()
	require.Error(t, <-finished)
	require.NoError(t, blocker.Commit(ctx))
	var got uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, `SELECT latest_version_id FROM latest_entry_version WHERE source_id=$1 AND name='blocked'`, source).Scan(&got))
	require.Equal(t, old, got)
	require.NoError(t, dbcore.ReconcileLatestVersions(ctx, pool))
	require.NoError(t, pool.QueryRow(ctx, `SELECT latest_version_id FROM latest_entry_version WHERE source_id=$1 AND name='blocked'`, source).Scan(&got))
	require.NotEqual(t, old, got)
}

func versionAt(t *testing.T, pool *pgxpool.Pool) (uint, bool) {
	t.Helper()
	version, dirty, err := database.GetVersion(pool.Config().ConnString())
	require.NoError(t, err)
	return version, dirty
}

func migrateConn(t *testing.T, pool *pgxpool.Pool) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), pool.Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

// Before migration 25 a skill and a plugin sharing a name in one source shared
// one pointer (#656). Upgrading must give the skill its own pointer exactly once.
func TestUpgradeRepairsLegacyPointersOnce(t *testing.T) {
	t.Parallel()
	conn, cleanup := dbtest.SetupTestDBAt(t, 24)
	t.Cleanup(cleanup)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, conn.Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	source := repairTestSource(t, pool)
	skill := repairTestEntry(t, pool, source, "SKILL", "dup", "1.0.0")
	plugin := repairTestEntry(t, pool, source, "PLUGIN", "dup", "2.0.0")
	_, err = pool.Exec(ctx, `INSERT INTO latest_entry_version (source_id,name,version,latest_version_id)
		VALUES ($1,'dup','2.0.0',$2)`, source, plugin)
	require.NoError(t, err)

	require.NoError(t, database.MigrateUpTo(ctx, migrateConn(t, pool), database.LatestSchemaVersion))
	pending, err := dbcore.LatestRepairPending(ctx, pool)
	require.NoError(t, err)
	require.True(t, pending)

	repaired, err := dbcore.RepairPendingLatestVersions(ctx, pool)
	require.NoError(t, err)
	require.True(t, repaired)
	var got uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, `SELECT latest_version_id FROM latest_entry_version
		WHERE source_id=$1 AND entry_type='SKILL' AND name='dup'`, source).Scan(&got))
	require.Equal(t, skill, got)
	require.NoError(t, pool.QueryRow(ctx, `SELECT latest_version_id FROM latest_entry_version
		WHERE source_id=$1 AND entry_type='PLUGIN' AND name='dup'`, source).Scan(&got))
	require.Equal(t, plugin, got)

	// Later starts only read the mark and leave pointers alone.
	_, err = pool.Exec(ctx, `UPDATE latest_entry_version SET version='untouched' WHERE source_id=$1 AND entry_type='SKILL'`, source)
	require.NoError(t, err)
	repaired, err = dbcore.RepairPendingLatestVersions(ctx, pool)
	require.NoError(t, err)
	require.False(t, repaired)
	var version string
	require.NoError(t, pool.QueryRow(ctx, `SELECT version FROM latest_entry_version
		WHERE source_id=$1 AND entry_type='SKILL'`, source).Scan(&version))
	require.Equal(t, "untouched", version)
}

// Rolling back past migration 25 must succeed even when the new per-kind
// pointers cannot all fit the older (source, name) key, and leave a clean
// schema the older binary accepts.
func TestDowngradePastLatestKindAlwaysSucceeds(t *testing.T) {
	t.Parallel()
	pool := repairTestPool(t) // schema 26
	ctx := context.Background()
	conn := migrateConn(t, pool)
	require.NoError(t, database.MigrateUpTo(ctx, conn, database.LatestSchemaVersion))

	source := repairTestSource(t, pool)
	server := repairTestEntry(t, pool, source, "MCP", "shared", "1.0.0")
	skill := repairTestEntry(t, pool, source, "SKILL", "shared", "1.0.0")
	plugin := repairTestEntry(t, pool, source, "PLUGIN", "pair", "1.0.0")
	skill2 := repairTestEntry(t, pool, source, "SKILL", "pair", "1.0.0")
	for _, p := range []struct {
		kind, name string
		id         uuid.UUID
	}{{"MCP", "shared", server}, {"SKILL", "shared", skill}, {"PLUGIN", "pair", plugin}, {"SKILL", "pair", skill2}} {
		_, err := pool.Exec(ctx, `INSERT INTO latest_entry_version (source_id,entry_type,name,version,latest_version_id)
			VALUES ($1,$2,$3,'1.0.0',$4)`, source, p.kind, p.name, p.id)
		require.NoError(t, err)
	}
	_, err := pool.Exec(ctx, `UPDATE skill SET provenance='{"x":1}'`)
	require.NoError(t, err)

	require.NoError(t, database.MigrateDown(ctx, conn, int(database.LatestSchemaVersion)-24))
	version, dirty := versionAt(t, pool)
	require.Equal(t, uint(24), version)
	require.False(t, dirty)
	for name, want := range map[string]uuid.UUID{"shared": server, "pair": skill2} {
		var got uuid.UUID
		require.NoError(t, pool.QueryRow(ctx, `SELECT latest_version_id FROM latest_entry_version
			WHERE source_id=$1 AND name=$2`, source, name).Scan(&got))
		require.Equal(t, want, got, name)
	}

	// Upgrading again schedules a fresh repair.
	require.NoError(t, database.MigrateUpTo(ctx, conn, database.LatestSchemaVersion))
	repaired, err := dbcore.RepairPendingLatestVersions(ctx, pool)
	require.NoError(t, err)
	require.True(t, repaired)
	var pointers int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM latest_entry_version WHERE source_id=$1`, source).Scan(&pointers))
	require.Equal(t, 4, pointers)
}
