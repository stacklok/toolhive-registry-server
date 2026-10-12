package database

import (
	"testing"

	"github.com/stretchr/testify/require"

	registrydb "github.com/stacklok/toolhive-registry-server/internal/db"
)

func TestExposureMigrationIdentityOnlyAndDowngrade(t *testing.T) {
	t.Parallel()
	conn, _ := SetupTestDB(t)
	require.NoError(t, migrateTestTo(t, conn, 26))
	_, err := conn.Exec(t.Context(), `INSERT INTO source(name,source_type,source_config,claims)
 VALUES('retained','file','{}','{"private":"retained"}');
 INSERT INTO registry_sync(source_id,server_count,last_sync_hash,applied_generation)
 SELECT id,7,'retained',definition_generation FROM source`)
	require.NoError(t, err)
	var before string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT row_to_json(s)::text FROM source s`).Scan(&before))
	require.NoError(t, MigrateUp(t.Context(), conn))
	var after, identity string
	var ack *string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT row_to_json(s)::text FROM source s`).Scan(&after))
	require.Equal(t, before, after)
	var count, applied int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT server_count,applied_generation FROM registry_sync`).Scan(&count, &applied))
	require.Equal(t, 7, count)
	require.Equal(t, 1, applied)
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT database_id::text,acknowledgement_id::text
 FROM exposure_acknowledgement`).Scan(&identity, &ack))
	require.NotEmpty(t, identity)
	require.Nil(t, ack)
	require.NoError(t, MigrateUp(t.Context(), conn))
	var unchanged string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT database_id::text FROM exposure_acknowledgement`).Scan(&unchanged))
	require.Equal(t, identity, unchanged)
	require.NoError(t, migrateTestTo(t, conn, 26))
	require.NoError(t, MigrateUp(t.Context(), conn))
	revision := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	inventory, err := registrydb.InventoryExposure(t.Context(), conn, revision)
	require.NoError(t, err)
	_, err = registrydb.AcknowledgeExposure(t.Context(), conn, revision, inventory.Digest, true, true, false)
	require.NoError(t, err)
	require.ErrorContains(t, migrateTestTo(t, conn, 26), "refusing to discard exposure acknowledgement")
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT acknowledgement_id::text FROM exposure_acknowledgement`).Scan(&ack))
	require.NotNil(t, ack)
}
