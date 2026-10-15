package database

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	migratedb "github.com/golang-migrate/migrate/v4/database"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestMigrationDriverCloseCachesResult(t *testing.T) {
	t.Parallel()
	for _, unconfirmed := range []bool{false, true} {
		t.Run(map[bool]string{false: "confirmed", true: "unconfirmed"}[unconfirmed], func(t *testing.T) {
			t.Parallel()
			control, _ := SetupTestDBAt(t, 29)
			d, err := newMigrationDriver(t.Context(), control.Config().ConnString(), control)
			require.NoError(t, err)
			var probes atomic.Int32
			d.backendProbe = func(ctx context.Context, conn *pgx.Conn, pid uint32, start time.Time) (bool, error) {
				probes.Add(1)
				if unconfirmed {
					return false, context.DeadlineExceeded
				}
				return migrationBackendExists(ctx, conn, pid, start)
			}
			first := d.Close()
			if unconfirmed {
				require.ErrorIs(t, first, errMigrationBackendUnconfirmed)
			} else {
				require.NoError(t, first)
			}
			count := probes.Load()
			var wg sync.WaitGroup
			for range 4 {
				wg.Go(func() { require.Equal(t, first, d.Close()) })
			}
			wg.Wait()
			require.Equal(t, count, probes.Load(), "repeated Close performs no further observation or connection cleanup")
		})
	}
}

func TestMigrationDriverCloseRecycledPIDDoesNotSignalReplacement(t *testing.T) {
	t.Parallel()
	control, _ := SetupTestDBAt(t, 29)
	d, err := newMigrationDriver(t.Context(), control.Config().ConnString(), control)
	require.NoError(t, err)
	replacement, err := pgx.ConnectConfig(t.Context(), control.Config().Copy())
	require.NoError(t, err)
	defer replacement.Close(context.WithoutCancel(t.Context()))
	var replacementStart time.Time
	require.NoError(t, replacement.QueryRow(t.Context(), `SELECT backend_start FROM pg_stat_activity WHERE pid=pg_backend_pid()`).Scan(&replacementStart))
	require.NotEqual(t, d.backendStart, replacementStart)
	originalPID, originalStart := d.pid, d.backendStart
	// Simulate reuse without relying on the OS to recycle a PID in this test.
	d.pid = replacement.PgConn().PID()
	_, err = control.Exec(t.Context(), `SELECT pg_advisory_lock(915,31)`)
	require.NoError(t, err)
	finished := make(chan error, 1)
	go func() {
		_, err := replacement.Exec(t.Context(), `SELECT pg_advisory_lock(915,31); SELECT pg_advisory_unlock(915,31)`)
		finished <- err
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := control.QueryRow(t.Context(), `SELECT wait_event='advisory' FROM pg_stat_activity WHERE pid=$1`, d.pid).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, d.Close())
	exists, err := migrationBackendExists(t.Context(), control, originalPID, originalStart)
	require.NoError(t, err)
	require.False(t, exists)
	exists, err = migrationBackendExists(t.Context(), control, d.pid, replacementStart)
	require.NoError(t, err)
	require.True(t, exists, "replacement identity must remain alive")
	_, err = control.Exec(t.Context(), `SELECT pg_advisory_unlock(915,31)`)
	require.NoError(t, err)
	require.NoError(t, <-finished, "replacement query must not be cancelled or terminated")
	require.NoError(t, replacement.Ping(t.Context()))
}

func TestMigrationDriverLockState(t *testing.T) {
	t.Parallel()
	control, _ := SetupTestDBAt(t, 29)
	d, err := newMigrationDriver(t.Context(), control.Config().ConnString(), control)
	require.NoError(t, err)
	defer d.Close()
	require.ErrorIs(t, d.Unlock(), migratedb.ErrNotLocked)
	require.NoError(t, d.Lock())
	require.ErrorIs(t, d.Lock(), migratedb.ErrLocked)
	require.NoError(t, d.Unlock())
	require.ErrorIs(t, d.Unlock(), migratedb.ErrNotLocked)
	var acquired bool
	require.NoError(t, control.QueryRow(t.Context(), `SELECT pg_try_advisory_lock($1::text::bigint)`, d.lockID).Scan(&acquired))
	require.True(t, acquired, "repeated Lock must not increment the session lock count")
	_, err = control.Exec(t.Context(), `SELECT pg_advisory_unlock($1::text::bigint)`, d.lockID)
	require.NoError(t, err)
}
