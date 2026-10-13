package database

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	registrydb "github.com/stacklok/toolhive-registry-server/internal/db"
)

func TestRawCleanupRejectsClonedConsumedTarget(t *testing.T) {
	t.Parallel()
	original, _ := SetupTestDBAt(t, 29)
	clone, _ := SetupTestDBAt(t, 29)
	ctx := t.Context()
	_, err := original.Exec(ctx, `INSERT INTO source(name,source_type,claims) VALUES('history','file','{"team":"private"}')`)
	require.NoError(t, err)
	inventory, err := registrydb.InventoryExposure(ctx, original, consumedTargetRevision)
	require.NoError(t, err)
	ack, err := registrydb.AcknowledgeExposure(ctx, original, consumedTargetRevision, inventory.Digest, true, true, false)
	require.NoError(t, err)
	_, err = registrydb.RetireExposureClaims(ctx, original, consumedTargetRevision, ack.ID, ack.PostFenceDigest, true, true)
	require.NoError(t, err)
	require.NoError(t, registrydb.ConsumeExposure(ctx, original, consumedTargetRevision))
	// Clone the historical rows verbatim into a different actual database endpoint.
	for _, table := range []string{"source", "exposure_acknowledgement", "exposure_claims_archive"} {
		var rows string
		require.NoError(t, original.QueryRow(ctx, `SELECT json_agg(t)::text FROM `+pgx.Identifier{table}.Sanitize()+` t`).Scan(&rows))
		_, err = clone.Exec(ctx, `DELETE FROM `+pgx.Identifier{table}.Sanitize())
		require.NoError(t, err)
		_, err = clone.Exec(ctx, `INSERT INTO `+pgx.Identifier{table}.Sanitize()+` SELECT * FROM json_populate_recordset(NULL::`+pgx.Identifier{table}.Sanitize()+`, $1::json)`, rows)
		require.NoError(t, err)
	}
	var before, after, archiveBefore, archiveAfter string
	require.NoError(t, clone.QueryRow(ctx, `SELECT row_to_json(e)::text FROM exposure_acknowledgement e`).Scan(&before))
	require.NoError(t, clone.QueryRow(ctx, `SELECT json_agg(a ORDER BY row_table,row_id)::text FROM exposure_claims_archive a`).Scan(&archiveBefore))
	m, err := GetMigrate(clone.Config().ConnString())
	require.NoError(t, err)
	require.ErrorContains(t, m.Migrate(30), "consumed exposure target mismatch")
	sourceErr, driverErr := m.Close()
	require.NoError(t, sourceErr)
	require.NoError(t, driverErr)
	require.NoError(t, clone.QueryRow(ctx, `SELECT row_to_json(e)::text FROM exposure_acknowledgement e`).Scan(&after))
	require.Equal(t, before, after)
	require.NoError(t, clone.QueryRow(ctx, `SELECT json_agg(a ORDER BY row_table,row_id)::text FROM exposure_claims_archive a`).Scan(&archiveAfter))
	require.Equal(t, archiveBefore, archiveAfter)
	assertClaimsColumns(t, clone)
	version, dirty, err := GetVersion(clone.Config().ConnString())
	require.NoError(t, err)
	require.Equal(t, uint(30), version)
	require.True(t, dirty, "raw migrators can mark dirty before the SQL backstop refuses")
}

func TestCleanupSQLRequiresFreshContextOnlyForConsumedReceipt(t *testing.T) {
	t.Parallel()
	for _, consumed := range []bool{false, true} {
		t.Run(map[bool]string{false: "virgin", true: "consumed"}[consumed], func(t *testing.T) {
			t.Parallel()
			conn, _ := SetupTestDBAt(t, 29)
			if consumed {
				require.NoError(t, registrydb.ConsumeEmptyExposure(t.Context(), conn, consumedTargetRevision))
			}
			script, err := migrationsFS.ReadFile("migrations/000030_claims_schema_cleanup.up.sql")
			require.NoError(t, err)
			_, err = conn.Exec(t.Context(), string(script))
			if consumed {
				require.ErrorContains(t, err, "verification context absent")
				_, err = conn.Exec(t.Context(), `ROLLBACK`)
				require.NoError(t, err)
				assertClaimsColumns(t, conn)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func assertClaimsColumns(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	var columns int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM information_schema.columns
 WHERE table_schema='public' AND table_name IN ('source','registry','registry_entry') AND column_name='claims'`).Scan(&columns))
	require.Equal(t, 3, columns)
}

func TestMigrationConnectionAcquisitionDeadline(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err = GetMigrateContext(ctx, "postgres://test:test@"+listener.Addr().String()+"/test?sslmode=disable")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	select {
	case conn := <-accepted:
		require.NoError(t, conn.Close())
	case <-time.After(5 * time.Second):
		t.Fatal("migration connection was not attempted")
	}
}

func TestMigrationCancellationJoinsDriverBeforeRunnerUnlock(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"version-cancel", "version-deadline", "ddl-cancel", "control-loss-cancel"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			version := uint(29)
			if operation == "ddl-cancel" {
				version = 28
			}
			conn, _ := SetupTestDBAt(t, version)
			worker, err := pgx.ConnectConfig(t.Context(), conn.Config().Copy())
			require.NoError(t, err)
			defer worker.Close(context.WithoutCancel(t.Context()))
			tx, err := conn.Begin(t.Context())
			require.NoError(t, err)
			defer tx.Rollback(context.WithoutCancel(t.Context()))
			lock := `LOCK TABLE schema_migrations IN ACCESS EXCLUSIVE MODE`
			if operation == "ddl-cancel" {
				lock = `LOCK TABLE exposure_acknowledgement IN ACCESS SHARE MODE`
			}
			_, err = tx.Exec(t.Context(), lock)
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			if operation == "version-deadline" {
				cancel()
				ctx, cancel = context.WithTimeout(t.Context(), 2*time.Second)
			}
			defer cancel()
			finished := make(chan error, 1)
			go func() { finished <- MigrateUpTo(ctx, worker, 29) }()
			require.Eventually(t, func() bool {
				_, err := tx.Exec(t.Context(), `SELECT pg_stat_clear_snapshot()`)
				if err != nil {
					return false
				}
				var waiting bool
				err = tx.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
 WHERE datname=current_database() AND pid<>pg_backend_pid() AND pid<>$1 AND wait_event_type='Lock')`, worker.PgConn().PID()).Scan(&waiting)
				return err == nil && waiting
			}, 5*time.Second, 10*time.Millisecond)
			var acquired bool
			require.NoError(t, tx.QueryRow(t.Context(), `SELECT pg_try_advisory_lock(915,30)`).Scan(&acquired))
			require.False(t, acquired, "workflow lock remains held while the actual driver is blocked")
			if operation == "control-loss-cancel" {
				require.NoError(t, worker.Close(context.WithoutCancel(t.Context())))
				require.NoError(t, tx.QueryRow(t.Context(), `SELECT pg_try_advisory_lock(915,30)`).Scan(&acquired))
				require.False(t, acquired, "losing the caller session must not unlock the executing backend")
			}
			if operation != "version-deadline" {
				cancel()
			}
			select {
			case err := <-finished:
				if operation == "version-deadline" {
					require.ErrorIs(t, err, context.DeadlineExceeded)
				} else {
					require.ErrorIs(t, err, context.Canceled)
					if operation == "control-loss-cancel" {
						require.ErrorIs(t, err, errMigrationBackendUnconfirmed)
					}
				}
			case <-time.After(10 * time.Second):
				t.Fatal("migration driver did not stop after cancellation")
			}
			_, err = tx.Exec(t.Context(), `SELECT pg_stat_clear_snapshot()`)
			require.NoError(t, err)
			var active int
			require.NoError(t, tx.QueryRow(t.Context(), `SELECT count(*) FROM pg_stat_activity
 WHERE datname=current_database() AND pid<>pg_backend_pid() AND pid<>$1`, worker.PgConn().PID()).Scan(&active))
			if operation == "control-loss-cancel" {
				require.Eventually(t, func() bool {
					_, err := tx.Exec(t.Context(), `SELECT pg_stat_clear_snapshot()`)
					if err != nil {
						return false
					}
					err = tx.QueryRow(t.Context(), `SELECT count(*) FROM pg_stat_activity
 WHERE datname=current_database() AND pid<>pg_backend_pid()`).Scan(&active)
					return err == nil && active == 0
				}, 5*time.Second, 10*time.Millisecond)
			} else {
				require.Zero(t, active, "actual migration backend is gone before the runner returns")
			}
			require.NoError(t, tx.QueryRow(t.Context(), `SELECT pg_try_advisory_lock(915,30)`).Scan(&acquired))
			require.True(t, acquired, "a subsequent runner can acquire the workflow lock")
			_, err = tx.Exec(t.Context(), `SELECT pg_advisory_unlock(915,30)`)
			require.NoError(t, err)
			require.NoError(t, tx.Rollback(t.Context()))
			current, dirty, err := GetVersion(conn.Config().ConnString())
			require.NoError(t, err)
			if operation == "ddl-cancel" {
				require.Equal(t, uint(29), current)
				require.True(t, dirty)
				var added bool
				require.NoError(t, conn.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM information_schema.columns
 WHERE table_name='exposure_acknowledgement' AND column_name='consumed_target_digest')`).Scan(&added))
				require.False(t, added, "cancelled DDL does not continue after return")
			} else {
				require.Equal(t, uint(29), current)
				require.False(t, dirty)
				require.NoError(t, MigrateUpTo(t.Context(), conn, 29))
			}
		})
	}
}
