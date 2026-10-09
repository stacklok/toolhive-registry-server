package database

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/sync/state"
)

func TestLatestKindDownRejectsUnpointedCollision(t *testing.T) {
	t.Parallel()
	db, _ := SetupTestDB(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `INSERT INTO source(name,source_type,source_config,syncable) VALUES('unpointed','managed','{}',false)`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO registry_entry(source_id,entry_type,name)
 SELECT s.id,k.kind,'same' FROM source s CROSS JOIN (VALUES ('MCP'::entry_type),('SKILL'::entry_type)) k(kind) WHERE s.name='unpointed'`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO entry_version(entry_id,name,version)
 SELECT e.id,e.name,'1' FROM registry_entry e JOIN source s ON s.id=e.source_id WHERE s.name='unpointed'`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO latest_entry_version(source_id,entry_type,name,version,latest_version_id)
 SELECT e.source_id,e.entry_type,e.name,v.version,v.id FROM registry_entry e JOIN entry_version v ON v.entry_id=e.id
 JOIN source s ON s.id=e.source_id WHERE s.name='unpointed' AND e.entry_type='MCP'`)
	require.NoError(t, err)
	require.Error(t, MigrateDown(ctx, db, 2))
}

func TestLatestKindRoundTripAndPayloadGuard(t *testing.T) {
	t.Parallel()
	db, _ := SetupTestDB(t)
	ctx := t.Context()
	require.NoError(t, MigrateDown(ctx, db, 2))
	_, err := db.Exec(ctx, `INSERT INTO source(name,source_type,source_config,syncable) VALUES('roundtrip','managed','{}',false)`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO registry_entry(source_id,entry_type,name)
 SELECT id,'MCP','server' FROM source WHERE name='roundtrip'`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO entry_version(entry_id,name,version,description)
 SELECT id,'server','v1','retained' FROM registry_entry WHERE name='server'`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO mcp_server(version_id) SELECT id FROM entry_version WHERE name='server'`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO latest_entry_version(source_id,name,version,latest_version_id)
 SELECT e.source_id,e.name,v.version,v.id FROM registry_entry e JOIN entry_version v ON v.entry_id=e.id WHERE e.name='server'`)
	require.NoError(t, err)
	require.NoError(t, MigrateUp(ctx, db))
	var kind, description string
	require.NoError(t, db.QueryRow(ctx, `SELECT l.entry_type::text,v.description FROM latest_entry_version l
 JOIN entry_version v ON v.id=l.latest_version_id WHERE l.name='server'`).Scan(&kind, &description))
	require.Equal(t, "MCP", kind)
	require.Equal(t, "retained", description)
	require.NoError(t, MigrateDown(ctx, db, 2))
	require.NoError(t, db.QueryRow(ctx, `SELECT v.description FROM latest_entry_version l JOIN entry_version v
 ON v.id=l.latest_version_id WHERE l.name='server'`).Scan(&description))
	require.Equal(t, "retained", description)
	require.NoError(t, MigrateUp(ctx, db))
	require.NoError(t, db.QueryRow(ctx, `SELECT l.entry_type::text FROM latest_entry_version l WHERE l.name='server'`).Scan(&kind))
	require.Equal(t, "MCP", kind)
}

func TestLatestKindDownRejectsNewPayload(t *testing.T) {
	t.Parallel()
	db, _ := SetupTestDB(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `INSERT INTO source(name,source_type,source_config,syncable) VALUES('payload','managed','{}',false)`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO registry_entry(source_id,entry_type,name) SELECT id,'MCP','server' FROM source WHERE name='payload'`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO entry_version(entry_id,name,version) SELECT id,'server','v1' FROM registry_entry WHERE name='server'`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO mcp_server(version_id,schema_url) SELECT id,'https://example.org/schema' FROM entry_version WHERE name='server'`)
	require.NoError(t, err)
	require.Error(t, MigrateDown(ctx, db, 2), "nonempty new payload cannot be discarded")
}

func TestLatestKindMigrationAndRepair(t *testing.T) {
	t.Parallel()
	db, _ := SetupTestDB(t)
	ctx := t.Context()
	require.NoError(t, MigrateDown(ctx, db, 2))
	_, err := db.Exec(ctx, `INSERT INTO source(name,source_type,source_config,syncable) VALUES('mixed','managed','{}',false)`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO registry_entry(source_id,entry_type,name)
 SELECT s.id,k.kind,'same' FROM source s CROSS JOIN (VALUES ('MCP'::entry_type),('SKILL'::entry_type),('PLUGIN'::entry_type)) k(kind) WHERE s.name='mixed'`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO entry_version(entry_id,name,version)
 SELECT e.id,e.name,v.version FROM registry_entry e CROSS JOIN (VALUES ('custom'),('1.0.0'),('v1.0.0')) v(version) WHERE e.name='same'`)
	require.NoError(t, err)
	// The pre-upgrade key represents at most one kind; deliberately point it at
	// the wrong version to prove the Go repair replaces it after the migration.
	_, err = db.Exec(ctx, `INSERT INTO latest_entry_version(source_id,name,version,latest_version_id)
 SELECT e.source_id,e.name,v.version,v.id FROM registry_entry e JOIN entry_version v ON v.entry_id=e.id
 WHERE e.entry_type='MCP' AND v.version='custom'`)
	require.NoError(t, err)
	require.NoError(t, MigrateUp(ctx, db))
	pool, err := pgxpool.New(ctx, db.Config().ConnString())
	require.NoError(t, err)
	defer pool.Close()
	require.NoError(t, state.ReconcileLatestVersions(ctx, pool))
	rows, err := db.Query(ctx, `SELECT l.entry_type::text,l.version FROM latest_entry_version l JOIN source s ON s.id=l.source_id WHERE s.name='mixed' ORDER BY l.entry_type::text`)
	require.NoError(t, err)
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var kind, version string
		require.NoError(t, rows.Scan(&kind, &version))
		got[kind] = version
	}
	require.NoError(t, rows.Err())
	require.Equal(t, map[string]string{"MCP": "v1.0.0", "SKILL": "v1.0.0", "PLUGIN": "v1.0.0"}, got)
	var wrongID string
	require.NoError(t, db.QueryRow(ctx, `SELECT latest_version_id::text FROM latest_entry_version WHERE entry_type='MCP' AND name='same'`).Scan(&wrongID))
	_, err = db.Exec(ctx, `UPDATE latest_entry_version SET latest_version_id=$1 WHERE entry_type='SKILL' AND name='same'`, wrongID)
	require.Error(t, err, "pointer cannot cross kinds")
	// Rollback with multiple kinds sharing a name must refuse to lose pointers.
	require.Error(t, MigrateDown(ctx, db, 2))
}
