package database

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	registrydb "github.com/stacklok/toolhive-registry-server/internal/db"
)

func TestSchema30CleanupCancellationPreservesEvidence(t *testing.T) {
	t.Parallel()
	for _, deadline := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "deadline"}[deadline], func(t *testing.T) {
			t.Parallel()
			conn, _ := SetupTestDBAt(t, 29)
			ctx := t.Context()
			_, err := conn.Exec(ctx, `INSERT INTO source(name,source_type,claims) VALUES('history','file','{"team":"private"}')`)
			require.NoError(t, err)
			inventory, err := registrydb.InventoryExposure(ctx, conn, consumedTargetRevision)
			require.NoError(t, err)
			ack, err := registrydb.AcknowledgeExposure(ctx, conn, consumedTargetRevision, inventory.Digest, true, true, false)
			require.NoError(t, err)
			_, err = registrydb.RetireExposureClaims(ctx, conn, consumedTargetRevision, ack.ID, ack.PostFenceDigest, true, true)
			require.NoError(t, err)
			require.NoError(t, registrydb.ConsumeExposure(ctx, conn, consumedTargetRevision))
			var receiptBefore, archiveBefore, receiptAfter, archiveAfter string
			receiptQuery := `SELECT row_to_json(e)::text FROM exposure_acknowledgement e`
			archiveQuery := `SELECT json_agg(a ORDER BY row_table,row_id)::text FROM exposure_claims_archive a`
			require.NoError(t, conn.QueryRow(ctx, receiptQuery).Scan(&receiptBefore))
			require.NoError(t, conn.QueryRow(ctx, archiveQuery).Scan(&archiveBefore))
			// Block the first real schema-30 ALTER after its locked guard and
			// transaction-local context verification, not the schema-29 DDL.
			_, err = conn.Exec(ctx, `CREATE FUNCTION block_cleanup_ddl() RETURNS event_trigger LANGUAGE plpgsql AS $$
 BEGIN
   IF current_setting('toolhive.exposure_target_digest', true) IS DISTINCT FROM
      (SELECT consumed_target_digest FROM exposure_acknowledgement WHERE singleton) THEN
     RAISE EXCEPTION 'cleanup context was not installed on the executing connection';
   END IF;
   PERFORM pg_advisory_lock(915,31);
   PERFORM pg_advisory_unlock(915,31);
 END $$;
 CREATE EVENT TRIGGER block_cleanup_ddl ON ddl_command_start WHEN TAG IN ('ALTER TABLE')
 EXECUTE FUNCTION block_cleanup_ddl();
 SELECT pg_advisory_lock(915,31)`)
			require.NoError(t, err)
			worker, err := pgx.ConnectConfig(ctx, conn.Config().Copy())
			require.NoError(t, err)
			defer worker.Close(context.WithoutCancel(ctx))
			runCtx, cancel := context.WithCancel(ctx)
			if deadline {
				cancel()
				runCtx, cancel = context.WithTimeout(ctx, 2*time.Second)
			}
			defer cancel()
			finished := make(chan error, 1)
			go func() { finished <- MigrateUp(runCtx, worker) }()
			var pid uint32
			var start time.Time
			require.Eventually(t, func() bool {
				err := conn.QueryRow(ctx, `SELECT pid,backend_start FROM pg_stat_activity
 WHERE datname=current_database() AND pid<>pg_backend_pid() AND pid<>$1 AND wait_event='advisory'`, worker.PgConn().PID()).Scan(&pid, &start)
				return err == nil
			}, 5*time.Second, 10*time.Millisecond)
			if !deadline {
				cancel()
			}
			select {
			case err := <-finished:
				if deadline {
					require.ErrorIs(t, err, context.DeadlineExceeded)
				} else {
					require.ErrorIs(t, err, context.Canceled)
				}
				require.NotErrorIs(t, err, errMigrationBackendUnconfirmed)
			case <-time.After(10 * time.Second):
				t.Fatal("schema-30 cleanup did not join cancellation")
			}
			exists, err := migrationBackendExists(ctx, conn, pid, start)
			require.NoError(t, err)
			require.False(t, exists, "confirmed return must leave no executing migration query")
			_, err = conn.Exec(ctx, `SELECT pg_advisory_unlock(915,31)`)
			require.NoError(t, err)
			assertClaimsColumns(t, conn)
			require.NoError(t, conn.QueryRow(ctx, receiptQuery).Scan(&receiptAfter))
			require.Equal(t, receiptBefore, receiptAfter)
			require.NoError(t, conn.QueryRow(ctx, archiveQuery).Scan(&archiveAfter))
			require.Equal(t, archiveBefore, archiveAfter)
			version, dirty, err := GetVersion(conn.Config().ConnString())
			require.NoError(t, err)
			require.Equal(t, uint(30), version)
			require.True(t, dirty, "cancellation rolls back cleanup DDL, not the prior dirty mark")
		})
	}
}
