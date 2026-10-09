package database

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJobsMigrationLegacyRowsHaveNoAppliedBaseline(t *testing.T) {
	t.Parallel()
	db, _ := SetupTestDB(t)
	ctx := t.Context()
	require.NoError(t, MigrateDown(ctx, db, 1))
	_, err := db.Exec(ctx, `INSERT INTO source(name,source_type,source_config,syncable) VALUES('legacy-jobs','api','{}',true)`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO registry_sync(source_id,sync_status,last_sync_hash,last_applied_filter_hash,server_count)
 SELECT id,'COMPLETED','old-hash','old-filter',7 FROM source WHERE name='legacy-jobs'`)
	require.NoError(t, err)
	require.NoError(t, MigrateUp(ctx, db))
	var hash string
	var applied *int64
	require.NoError(t, db.QueryRow(ctx, `SELECT last_sync_hash,applied_generation FROM registry_sync
 WHERE source_id=(SELECT id FROM source WHERE name='legacy-jobs')`).Scan(&hash, &applied))
	require.Equal(t, "old-hash", hash)
	require.Nil(t, applied)
}

func TestJobsMigrationDownRefusesActiveLease(t *testing.T) {
	t.Parallel()
	db, _ := SetupTestDB(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `INSERT INTO source(name,source_type,source_config,syncable) VALUES('leased','api','{}',true)`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO registry_sync(source_id,sync_status,lease_id,lease_expires_at,lease_generation)
 SELECT id,'IN_PROGRESS',gen_random_uuid(),clock_timestamp()+interval '1 hour',definition_generation
 FROM source WHERE name='leased'`)
	require.NoError(t, err)
	require.ErrorContains(t, MigrateDown(ctx, db, 1), "drain active sync jobs")
}
