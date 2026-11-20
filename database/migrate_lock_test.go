package database_test

import (
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/database/dbtest"
)

// Several replicas may start against the same database at once. A replica
// that has connected may find that another one has since started migrating:
// it holds golang-migrate's lock and has marked the version dirty while it
// runs. Reading the version must wait for that migration instead of
// reporting it as one that failed part-way.
func TestMigrationCheckWaitsForAnotherReplica(t *testing.T) {
	t.Parallel()
	conn, _ := dbtest.SetupTestDBAt(t, 23)
	ctx := t.Context()

	connected, ready := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- database.CheckBeforeMigrating(ctx, conn, connected, ready) }()
	<-connected

	// Once this replica has connected, the other one starts migrating, through
	// golang-migrate's own driver.
	other, err := (&postgres.Postgres{}).Open(conn.Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(func() { _ = other.Close() })
	require.NoError(t, other.Lock())
	require.NoError(t, other.SetVersion(23, true))
	close(ready)

	select {
	case err := <-done:
		t.Fatalf("the version was read while another replica held the migration lock: %v", err)
	case <-time.After(2 * time.Second):
	}

	// The other replica finishes its migration and releases the lock.
	require.NoError(t, other.SetVersion(23, false))
	require.NoError(t, other.Unlock())
	select {
	case err = <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("the check did not finish after the other replica released the migration lock")
	}
	require.NoError(t, err)
}
