package database

import (
	"testing"

	"github.com/stretchr/testify/require"

	registrydb "github.com/stacklok/toolhive-registry-server/internal/db"
)

func TestExposureRetirementMigrationPreservesReviewAndCatalog(t *testing.T) {
	t.Parallel()
	conn, _ := SetupTestDBAt(t, 29)
	require.NoError(t, migrateTestTo(t, conn, 27))
	_, err := conn.Exec(t.Context(), `INSERT INTO source(name,source_type,source_config,claims)
 VALUES('retained','file','{}','{"private":"retained"}');
 INSERT INTO registry_sync(source_id,server_count,last_sync_hash,applied_generation)
 SELECT id,7,'retained',definition_generation FROM source`)
	require.NoError(t, err)
	revision := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	inventory, err := registrydb.InventoryExposure(t.Context(), conn, revision)
	require.NoError(t, err, "schema 27 remains compatible")
	ack, err := registrydb.AcknowledgeExposure(t.Context(), conn, revision, inventory.Digest, true, true, false)
	require.NoError(t, err)
	_, err = registrydb.RetireExposureClaims(t.Context(), conn, revision, ack.ID, ack.PostFenceDigest, true, true)
	require.ErrorContains(t, err, "requires clean schema version 28")
	before, err := registrydb.InventoryExposure(t.Context(), conn, revision)
	require.NoError(t, err)
	require.NoError(t, MigrateUpTo(t.Context(), conn, 29))
	after, err := registrydb.InventoryExposure(t.Context(), conn, revision)
	require.NoError(t, err)
	require.Equal(t, before, after, "bookkeeping alone must not invalidate canonical format 1")
	var count int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM exposure_claims_archive`).Scan(&count))
	require.Zero(t, count)
	var dormant bool
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT retirement_id IS NULL AND consumed_at IS NULL
 FROM exposure_acknowledgement`).Scan(&dormant))
	require.True(t, dormant)
	// New down permits preserving a pending ack; historical 27 down still refuses it.
	require.NoError(t, migrateTestTo(t, conn, 27))
	require.NoError(t, MigrateUpTo(t.Context(), conn, 29))
	_, err = registrydb.RetireExposureClaims(t.Context(), conn, revision, ack.ID, ack.PostFenceDigest, true, true)
	require.NoError(t, err)
	require.ErrorContains(t, migrateTestTo(t, conn, 27), "refusing to discard exposure retirement")
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM exposure_claims_archive`).Scan(&count))
	require.Equal(t, 1, count)
}

func TestExposureRetirementDownRefusesArchiveOrConsumption(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"archive", "consumption"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			conn, _ := SetupTestDBAt(t, 29)
			if state == "archive" {
				_, err := conn.Exec(t.Context(), `INSERT INTO exposure_claims_archive
 (retirement_id,row_table,row_id,name,ownership,claims_sql_null)
 VALUES(gen_random_uuid(),'registry',gen_random_uuid(),'retained','API',true)`)
				require.NoError(t, err)
			} else {
				revision := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				inventory, err := registrydb.InventoryExposure(t.Context(), conn, revision)
				require.NoError(t, err)
				_, err = registrydb.AcknowledgeExposure(t.Context(), conn, revision, inventory.Digest, false, true, true)
				require.NoError(t, err)
				require.NoError(t, registrydb.ConsumeExposure(t.Context(), conn, revision))
				_, err = registrydb.AcknowledgeExposure(t.Context(), conn, revision, inventory.Digest, false, true, true)
				require.ErrorContains(t, err, "evidence cannot be replaced")
			}
			message := "refusing to discard exposure retirement"
			if state == "consumption" {
				message = "refusing to discard exposure consumed target evidence"
			}
			require.ErrorContains(t, migrateTestTo(t, conn, 27), message)
		})
	}
}

func TestExposureArchiveNarrowConstraints(t *testing.T) {
	t.Parallel()
	conn, _ := SetupTestDBAt(t, 29)
	for _, statement := range []string{
		`INSERT INTO exposure_claims_archive(retirement_id,row_table,row_id,name,claims_sql_null)
 VALUES(gen_random_uuid(),'arbitrary',gen_random_uuid(),'bad',true)`,
		`INSERT INTO exposure_claims_archive(retirement_id,row_table,row_id,name,claims_sql_null,claims)
 VALUES(gen_random_uuid(),'registry',gen_random_uuid(),'bad',true,'null')`,
		`INSERT INTO exposure_claims_archive(retirement_id,row_table,row_id,name,ownership,claims_sql_null,kind)
 VALUES(gen_random_uuid(),'source',gen_random_uuid(),'bad','API',true,'file')`,
		`UPDATE exposure_acknowledgement SET consumed_at=clock_timestamp()`,
		`UPDATE exposure_acknowledgement SET retirement_id=gen_random_uuid()`,
	} {
		_, err := conn.Exec(t.Context(), statement)
		require.Error(t, err)
		var pgerr interface{ SQLState() string }
		require.ErrorAs(t, err, &pgerr)
		require.Equal(t, "23514", pgerr.SQLState())
	}
	var fks int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM pg_constraint
 WHERE conrelid='exposure_claims_archive'::regclass AND contype='f'`).Scan(&fks))
	require.Zero(t, fks)
	var version int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT version FROM schema_migrations`).Scan(&version))
	require.Equal(t, 29, version)
	_, err := conn.Exec(t.Context(), `UPDATE schema_migrations SET version=31`)
	require.NoError(t, err)
	_, err = registrydb.InventoryExposure(t.Context(), conn, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	require.ErrorContains(t, err, "requires clean schema version 27, 28 or 29")
	// Restore only the fixture's version so its cleanup can proceed normally.
	_, err = conn.Exec(t.Context(), `UPDATE schema_migrations SET version=29`)
	require.NoError(t, err)
}

func TestExposureMigrationIdentityOnlyAndDowngrade(t *testing.T) {
	t.Parallel()
	conn, _ := SetupTestDBAt(t, 29)
	require.NoError(t, migrateTestTo(t, conn, 26))
	_, err := conn.Exec(t.Context(), `INSERT INTO source(name,source_type,source_config,claims)
 VALUES('retained','file','{}','{"private":"retained"}');
 INSERT INTO registry_sync(source_id,server_count,last_sync_hash,applied_generation)
 SELECT id,7,'retained',definition_generation FROM source`)
	require.NoError(t, err)
	var before string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT row_to_json(s)::text FROM source s`).Scan(&before))
	require.NoError(t, MigrateUpTo(t.Context(), conn, 29))
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
	require.NoError(t, MigrateUpTo(t.Context(), conn, 29))
	var unchanged string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT database_id::text FROM exposure_acknowledgement`).Scan(&unchanged))
	require.Equal(t, identity, unchanged)
	require.NoError(t, migrateTestTo(t, conn, 26))
	require.NoError(t, MigrateUpTo(t.Context(), conn, 29))
	revision := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	inventory, err := registrydb.InventoryExposure(t.Context(), conn, revision)
	require.NoError(t, err)
	_, err = registrydb.AcknowledgeExposure(t.Context(), conn, revision, inventory.Digest, true, true, false)
	require.NoError(t, err)
	require.ErrorContains(t, migrateTestTo(t, conn, 26), "refusing to discard exposure acknowledgement")
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT acknowledgement_id::text FROM exposure_acknowledgement`).Scan(&ack))
	require.NotNil(t, ack)
}
