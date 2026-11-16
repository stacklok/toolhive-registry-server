package database

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	registrydb "github.com/stacklok/toolhive-registry-server/internal/db"
)

func TestEmptyExposureCannotApproveArchiveHistory(t *testing.T) {
	t.Parallel()
	for _, version := range []uint{29, 30} {
		for _, pending := range []bool{false, true} {
			t.Run(fmt.Sprintf("schema-%d-pending-%t", version, pending), func(t *testing.T) {
				t.Parallel()
				conn, _ := SetupTestDBAt(t, version)
				inventory, err := registrydb.InventoryExposure(t.Context(), conn, consumedTargetRevision)
				require.NoError(t, err)
				if pending {
					_, err = registrydb.AcknowledgeExposure(t.Context(), conn, consumedTargetRevision, inventory.Digest, false, true, true)
					require.NoError(t, err)
				}
				_, err = conn.Exec(t.Context(), `INSERT INTO exposure_claims_archive(retirement_id,row_table,row_id,name,ownership,claims_sql_null)
 VALUES(gen_random_uuid(),'registry',gen_random_uuid(),'historical','API',true)`)
				require.NoError(t, err)
				var before, after string
				require.NoError(t, conn.QueryRow(t.Context(), `SELECT row_to_json(e)::text FROM exposure_acknowledgement e`).Scan(&before))
				_, err = registrydb.AcknowledgeExposure(t.Context(), conn, consumedTargetRevision, inventory.Digest, false, true, true)
				require.ErrorContains(t, err, "archived catalog history")
				require.ErrorContains(t, registrydb.ConsumeEmptyExposure(t.Context(), conn, consumedTargetRevision), "archived catalog history")
				if pending {
					require.ErrorContains(t, registrydb.ConsumeExposure(t.Context(), conn, consumedTargetRevision), "archived catalog history")
				}
				require.NoError(t, conn.QueryRow(t.Context(), `SELECT row_to_json(e)::text FROM exposure_acknowledgement e`).Scan(&after))
				require.Equal(t, before, after)
			})
		}
	}
}

func TestConcurrentFreshMigrateUpNeverDowngrades(t *testing.T) {
	t.Parallel()
	conn, _ := SetupTestDBContainer(t, t.Context())
	start := make(chan struct{})
	finished := make(chan error, 4)
	for range 4 {
		worker, err := pgx.ConnectConfig(t.Context(), conn.Config().Copy())
		require.NoError(t, err)
		go func() {
			defer worker.Close(context.WithoutCancel(t.Context()))
			<-start
			finished <- MigrateUp(t.Context(), worker)
		}()
	}
	close(start)
	for range 4 {
		require.NoError(t, <-finished)
	}
	version, dirty, err := GetVersion(conn.Config().ConnString())
	require.NoError(t, err)
	require.Equal(t, uint(30), version)
	require.False(t, dirty)
	var virgin bool
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT inventory_version=2 AND acknowledgement_id IS NULL
 FROM exposure_acknowledgement`).Scan(&virgin))
	require.True(t, virgin, "concurrent schema application never grants consent")
}

func TestSchema30PendingFormatOneCannotBeRebound(t *testing.T) {
	t.Parallel()
	conn, _ := SetupTestDB(t)
	inventory, err := registrydb.InventoryExposure(t.Context(), conn, consumedTargetRevision)
	require.NoError(t, err)
	require.Equal(t, 2, inventory.Version)
	_, err = registrydb.AcknowledgeExposure(t.Context(), conn, consumedTargetRevision, inventory.Digest, false, true, true)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `UPDATE exposure_acknowledgement SET inventory_version=1`)
	require.NoError(t, err)
	var before, after string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT row_to_json(e)::text FROM exposure_acknowledgement e`).Scan(&before))
	require.Error(t, registrydb.ConsumeEmptyExposure(t.Context(), conn, consumedTargetRevision))
	_, err = registrydb.AcknowledgeExposure(t.Context(), conn, consumedTargetRevision, inventory.Digest, false, true, true)
	require.ErrorContains(t, err, "cannot be rebound")
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT row_to_json(e)::text FROM exposure_acknowledgement e`).Scan(&after))
	require.Equal(t, before, after)
}

func TestClaimsCleanupRefusalLeavesClean29(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"claimed", "json-null", "empty-object", "unlabeled", "pending-empty", "pending-existing", "retired", "live-current", "live-stale", "archive"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			conn, _ := SetupTestDBAt(t, 29)
			ctx := t.Context()
			switch state {
			case "pending-empty":
				inventory, err := registrydb.InventoryExposure(ctx, conn, consumedTargetRevision)
				require.NoError(t, err)
				_, err = registrydb.AcknowledgeExposure(ctx, conn, consumedTargetRevision, inventory.Digest, false, true, true)
				require.NoError(t, err)
			case "archive":
				_, err := conn.Exec(ctx, `INSERT INTO exposure_claims_archive(retirement_id,row_table,row_id,name,ownership,claims_sql_null)
 VALUES(gen_random_uuid(),'registry',gen_random_uuid(),'retained','API',true)`)
				require.NoError(t, err)
			default:
				_, err := conn.Exec(ctx, `INSERT INTO source(name,source_type,source_config,syncable,creation_type)
 VALUES('historical','managed','{}',false,'API')`)
				require.NoError(t, err)
				switch state {
				case "claimed", "json-null", "empty-object":
					claims := map[string]string{"claimed": `{"team":"private"}`, "json-null": "null", "empty-object": "{}"}[state]
					_, err = conn.Exec(ctx, `UPDATE source SET claims=$1::jsonb`, claims)
					require.NoError(t, err)
				case "pending-existing", "retired":
					inventory, err := registrydb.InventoryExposure(ctx, conn, consumedTargetRevision)
					require.NoError(t, err)
					ack, err := registrydb.AcknowledgeExposure(ctx, conn, consumedTargetRevision, inventory.Digest, true, true, false)
					require.NoError(t, err)
					if state == "retired" {
						_, err = registrydb.RetireExposureClaims(ctx, conn, consumedTargetRevision, ack.ID, ack.PostFenceDigest, true, true)
						require.NoError(t, err)
					}
				case "live-current", "live-stale":
					inventory, err := registrydb.InventoryExposure(ctx, conn, consumedTargetRevision)
					require.NoError(t, err)
					ack, err := registrydb.AcknowledgeExposure(ctx, conn, consumedTargetRevision, inventory.Digest, true, true, false)
					require.NoError(t, err)
					_, err = registrydb.RetireExposureClaims(ctx, conn, consumedTargetRevision, ack.ID, ack.PostFenceDigest, true, true)
					require.NoError(t, err)
					require.NoError(t, registrydb.ConsumeExposure(ctx, conn, consumedTargetRevision))
					_, err = conn.Exec(ctx, `INSERT INTO registry_sync(source_id,lease_id,lease_generation,lease_expires_at)
 SELECT id,gen_random_uuid(),CASE WHEN $1 THEN 0 ELSE definition_generation END,clock_timestamp()+interval '1 hour' FROM source`, state == "live-stale")
					require.NoError(t, err)
				}
			}
			var before, after string
			require.NoError(t, conn.QueryRow(ctx, `SELECT row_to_json(e)::text FROM exposure_acknowledgement e`).Scan(&before))
			require.Error(t, MigrateUp(ctx, conn))
			version, dirty, err := GetVersion(conn.Config().ConnString())
			require.NoError(t, err)
			require.Equal(t, uint(29), version)
			require.False(t, dirty)
			require.NoError(t, conn.QueryRow(ctx, `SELECT row_to_json(e)::text FROM exposure_acknowledgement e`).Scan(&after))
			require.Equal(t, before, after)
		})
	}
}

func TestClaimsCleanupPreservesConsumedEvidence(t *testing.T) {
	t.Parallel()
	conn, _ := SetupTestDBAt(t, 29)
	ctx := t.Context()
	_, err := conn.Exec(ctx, `INSERT INTO source(name,source_type,claims) VALUES('history','file','{"team":"private"}');
 INSERT INTO registry(name,claims) VALUES('history','null');
 INSERT INTO registry_entry(source_id,entry_type,name,claims) SELECT id,'MCP','com.example/server','{}' FROM source`)
	require.NoError(t, err)
	inventory, err := registrydb.InventoryExposure(ctx, conn, consumedTargetRevision)
	require.NoError(t, err)
	ack, err := registrydb.AcknowledgeExposure(ctx, conn, consumedTargetRevision, inventory.Digest, true, true, false)
	require.NoError(t, err)
	_, err = registrydb.RetireExposureClaims(ctx, conn, consumedTargetRevision, ack.ID, ack.PostFenceDigest, true, true)
	require.NoError(t, err)
	require.NoError(t, registrydb.ConsumeExposure(ctx, conn, consumedTargetRevision))
	var before, after, archiveBefore, archiveAfter string
	require.NoError(t, conn.QueryRow(ctx, `SELECT row_to_json(e)::text FROM exposure_acknowledgement e`).Scan(&before))
	archiveQuery := `SELECT json_agg(a ORDER BY row_table,row_id)::text FROM exposure_claims_archive a`
	require.NoError(t, conn.QueryRow(ctx, archiveQuery).Scan(&archiveBefore))
	require.NoError(t, MigrateUp(ctx, conn))
	require.NoError(t, conn.QueryRow(ctx, `SELECT row_to_json(e)::text FROM exposure_acknowledgement e`).Scan(&after))
	require.Equal(t, before, after)
	require.NoError(t, conn.QueryRow(ctx, archiveQuery).Scan(&archiveAfter))
	require.Equal(t, archiveBefore, archiveAfter)
	require.NoError(t, registrydb.ConsumeExposure(ctx, conn, consumedTargetRevision))
	require.Error(t, MigrateUpTo(ctx, conn, 29))
	require.Error(t, MigrateDown(ctx, conn, 1))
	version, dirty, err := GetVersion(conn.Config().ConnString())
	require.NoError(t, err)
	require.Equal(t, uint(30), version)
	require.False(t, dirty)
}

func TestFreshClaimsFreeSchemaRequiresExplicitBootstrap(t *testing.T) {
	t.Parallel()
	conn, _ := SetupTestDB(t)
	ctx := t.Context()
	require.Error(t, registrydb.ConsumeExposure(ctx, conn, consumedTargetRevision))
	var virgin bool
	require.NoError(t, conn.QueryRow(ctx, `SELECT inventory_version=2 AND acknowledgement_id IS NULL FROM exposure_acknowledgement`).Scan(&virgin))
	require.True(t, virgin)
	require.NoError(t, registrydb.ConsumeEmptyExposure(ctx, conn, consumedTargetRevision))
	require.NoError(t, registrydb.ConsumeExposure(ctx, conn, consumedTargetRevision))
	_, err := conn.Exec(ctx, `UPDATE exposure_acknowledgement SET database_id=gen_random_uuid()`)
	require.NoError(t, err)
	require.ErrorContains(t, registrydb.ConsumeExposure(ctx, conn, consumedTargetRevision), "target mismatch")
}

func TestFreshClaimsFreeBootstrapConcurrentRestarts(t *testing.T) {
	t.Parallel()
	conn, _ := SetupTestDB(t)
	start := make(chan struct{})
	finished := make(chan error, 4)
	for range 4 {
		worker, err := pgx.ConnectConfig(t.Context(), conn.Config().Copy())
		require.NoError(t, err)
		go func() {
			defer worker.Close(context.WithoutCancel(t.Context()))
			<-start
			err := registrydb.ConsumeEmptyExposure(t.Context(), worker, consumedTargetRevision)
			if err == nil {
				_, err = worker.Exec(t.Context(), `INSERT INTO registry(name,creation_type)
 VALUES('configured','CONFIG') ON CONFLICT DO NOTHING`)
			}
			finished <- err
		}()
	}
	close(start)
	for range 4 {
		require.NoError(t, <-finished)
	}
	require.NoError(t, registrydb.ConsumeEmptyExposure(t.Context(), conn,
		"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))
}

func TestCleanupTargetCloneRefusesAtClean29(t *testing.T) {
	t.Parallel()
	conn, _ := SetupTestDBAt(t, 29)
	require.NoError(t, registrydb.ConsumeEmptyExposure(t.Context(), conn, consumedTargetRevision))
	_, err := conn.Exec(t.Context(), `UPDATE exposure_acknowledgement SET database_id=gen_random_uuid()`)
	require.NoError(t, err)
	require.ErrorContains(t, MigrateUp(t.Context(), conn), "target mismatch")
	version, dirty, err := GetVersion(conn.Config().ConnString())
	require.NoError(t, err)
	require.Equal(t, uint(29), version)
	require.False(t, dirty)
}

func TestCleanupSQLBackstopRollsBackButRawMigratorMarksDirty(t *testing.T) {
	t.Parallel()
	conn, _ := SetupTestDBAt(t, 29)
	_, err := conn.Exec(t.Context(), `INSERT INTO source(name,source_type) VALUES('unreviewed','file')`)
	require.NoError(t, err)
	m, err := GetMigrate(conn.Config().ConnString())
	require.NoError(t, err)
	defer m.Close()
	require.ErrorContains(t, m.Migrate(30), "finalize exposure offline")
	// Close the owned driver before independently inspecting migration state.
	// The SQL guard rolls back DDL, but cannot undo the prior dirty mark.
	sourceErr, databaseErr := m.Close()
	require.NoError(t, sourceErr)
	require.NoError(t, databaseErr)
	version, dirty, err := GetVersion(conn.Config().ConnString())
	require.NoError(t, err)
	require.Equal(t, uint(30), version)
	require.True(t, dirty, "raw migration guard cannot undo golang-migrate's prior dirty mark")
	var columns int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM information_schema.columns
 WHERE table_schema='public' AND table_name IN ('source','registry','registry_entry') AND column_name='claims'`).Scan(&columns))
	require.Equal(t, 3, columns, "refused cleanup DDL rolled back")
}
