package database

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManagedSingletonMigrationRejectsExistingDuplicates(t *testing.T) {
	t.Parallel()
	db, _ := SetupTestDB(t)
	ctx := t.Context()
	require.NoError(t, MigrateDown(ctx, db, 3))
	_, err := db.Exec(ctx, `INSERT INTO source(name,source_type,source_config,syncable,creation_type) VALUES
 ('managed-one','managed','{}',false,'CONFIG'),('managed-two','managed','{}',false,'API'),
 ('legal','file','{"data":"{}"}',false,'API')`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO registry(name,creation_type) VALUES('legal-view','API')`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO registry_source(registry_id,source_id) SELECT r.id,s.id FROM registry r,source s WHERE r.name='legal-view' AND s.name='legal'`)
	require.NoError(t, err)
	require.Error(t, MigrateUp(ctx, db))
	var sources, links int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM source WHERE name IN ('managed-one','managed-two','legal')`).Scan(&sources))
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM registry_source rs JOIN registry r ON r.id=rs.registry_id JOIN source s ON s.id=rs.source_id WHERE r.name='legal-view' AND s.name='legal'`).Scan(&links))
	require.Equal(t, 3, sources)
	require.Equal(t, 1, links)
}
