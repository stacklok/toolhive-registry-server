package database

import (
	"testing"

	"github.com/stretchr/testify/require"

	registrydb "github.com/stacklok/toolhive-registry-server/internal/db"
)

const consumedTargetRevision = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestExposureConsumedTargetMigrationPreservesPendingEvidence(t *testing.T) {
	t.Parallel()
	for _, retired := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "retired"}[retired], func(t *testing.T) {
			t.Parallel()
			conn, _ := SetupTestDB(t)
			require.NoError(t, migrateTestTo(t, conn, 28))
			_, err := conn.Exec(t.Context(), `INSERT INTO source(name,source_type,claims)
 VALUES('retained','file','{"team":"private-sentinel"}')`)
			require.NoError(t, err)
			inventory, err := registrydb.InventoryExposure(t.Context(), conn, consumedTargetRevision)
			require.NoError(t, err)
			ack, err := registrydb.AcknowledgeExposure(t.Context(), conn, consumedTargetRevision, inventory.Digest, true, true, false)
			require.NoError(t, err)
			if retired {
				_, err = registrydb.RetireExposureClaims(t.Context(), conn, consumedTargetRevision, ack.ID, ack.PostFenceDigest, true, true)
				require.NoError(t, err)
			}
			var before, after string
			require.NoError(t, conn.QueryRow(t.Context(), `SELECT row_to_json(e)::text FROM exposure_acknowledgement e`).Scan(&before))
			inventory, err = registrydb.InventoryExposure(t.Context(), conn, consumedTargetRevision)
			require.NoError(t, err)
			require.NoError(t, MigrateUp(t.Context(), conn))
			require.NoError(t, conn.QueryRow(t.Context(), `SELECT row_to_json(e)::text FROM
 (SELECT singleton,database_id,inventory_version,acknowledgement_id,acknowledgement_mode,
 reviewed_digest,post_fence_digest,config_revision,acknowledged_at,retirement_id,
 post_retirement_digest,retired_at,consumed_at FROM exposure_acknowledgement) e`).Scan(&after))
			require.Equal(t, before, after, "pending receipt fields must be byte-identical")
			current, err := registrydb.InventoryExposure(t.Context(), conn, consumedTargetRevision)
			require.NoError(t, err)
			require.Equal(t, inventory, current)
			var absent bool
			require.NoError(t, conn.QueryRow(t.Context(), `SELECT consumed_target_digest IS NULL AND consumed_at IS NULL
 FROM exposure_acknowledgement`).Scan(&absent))
			require.True(t, absent)
			require.NoError(t, migrateTestTo(t, conn, 28), "pending and retired evidence survives downgrade to 28")
			require.NoError(t, conn.QueryRow(t.Context(), `SELECT row_to_json(e)::text FROM exposure_acknowledgement e`).Scan(&after))
			require.Equal(t, before, after)
		})
	}
}

func TestExposureConsumedTargetMigrationRefusesUnsupportedConsumption(t *testing.T) {
	t.Parallel()
	conn, _ := SetupTestDB(t)
	require.NoError(t, migrateTestTo(t, conn, 28))
	inventory, err := registrydb.InventoryExposure(t.Context(), conn, consumedTargetRevision)
	require.NoError(t, err)
	_, err = registrydb.AcknowledgeExposure(t.Context(), conn, consumedTargetRevision, inventory.Digest, false, true, true)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `UPDATE exposure_acknowledgement SET consumed_at=clock_timestamp()`)
	require.NoError(t, err)
	var before, after string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT row_to_json(e)::text FROM exposure_acknowledgement e`).Scan(&before))
	require.ErrorContains(t, MigrateUp(t.Context(), conn), "consumed exposure state lacks target evidence")
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT row_to_json(e)::text FROM exposure_acknowledgement e`).Scan(&after))
	require.Equal(t, before, after)
	var exists bool
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM information_schema.columns
 WHERE table_schema='public' AND table_name='exposure_acknowledgement' AND column_name='consumed_target_digest')`).Scan(&exists))
	require.False(t, exists, "failed migration must not manufacture target evidence")
}

func TestExposureConsumedTargetConstraintAndDownRefusal(t *testing.T) {
	t.Parallel()
	conn, _ := SetupTestDB(t)
	inventory, err := registrydb.InventoryExposure(t.Context(), conn, consumedTargetRevision)
	require.NoError(t, err)
	_, err = registrydb.AcknowledgeExposure(t.Context(), conn, consumedTargetRevision, inventory.Digest, false, true, true)
	require.NoError(t, err)
	for _, statement := range []string{
		`UPDATE exposure_acknowledgement SET consumed_at=clock_timestamp()`,
		`UPDATE exposure_acknowledgement SET consumed_target_digest='sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'`,
		`UPDATE exposure_acknowledgement SET consumed_at=clock_timestamp(),consumed_target_digest=''`,
		`UPDATE exposure_acknowledgement SET consumed_at=clock_timestamp(),consumed_target_digest='sha256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA'`,
		`UPDATE exposure_acknowledgement SET consumed_at=clock_timestamp(),consumed_target_digest='sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'`,
		`UPDATE exposure_acknowledgement SET consumed_at=clock_timestamp(),consumed_target_digest='sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n'`,
	} {
		_, err := conn.Exec(t.Context(), statement)
		require.Error(t, err)
		var pgerr interface{ SQLState() string }
		require.ErrorAs(t, err, &pgerr)
		require.Equal(t, "23514", pgerr.SQLState())
	}
	require.NoError(t, registrydb.ConsumeExposure(t.Context(), conn, consumedTargetRevision))
	var before, after string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT row_to_json(e)::text FROM exposure_acknowledgement e`).Scan(&before))
	require.ErrorContains(t, migrateTestTo(t, conn, 28), "refusing to discard exposure consumed target evidence")
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT row_to_json(e)::text FROM exposure_acknowledgement e`).Scan(&after))
	require.Equal(t, before, after)
}
