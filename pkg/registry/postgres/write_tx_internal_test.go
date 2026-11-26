package postgres

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database/dbtest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// Exhausted serialization retries are reported as ErrContention, which still
// matches ErrConflict so existing retry checks keep working, while a genuine
// duplicate is ErrConflict without ErrContention.
func TestWriteTxContention(t *testing.T) {
	t.Parallel()
	conn, _ := dbtest.SetupTestDB(t)
	pool, err := pgxpool.New(t.Context(), conn.Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	for _, tc := range []struct {
		name       string
		failure    error
		attempts   int
		contention bool
	}{
		{name: "retryable every time", failure: retryable{}, attempts: 5, contention: true},
		{name: "serialization failure every time", failure: &pgconn.PgError{Code: "40001"}, attempts: 5, contention: true},
		{name: "deadlock every time", failure: &pgconn.PgError{Code: "40P01"}, attempts: 5, contention: true},
		{name: "duplicate key", failure: &pgconn.PgError{Code: "23505"}, attempts: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			err := writeTx(t.Context(), pool, false, func(pgx.Tx) error {
				calls++
				return tc.failure
			})
			require.Equal(t, tc.attempts, calls)
			require.ErrorIs(t, err, persistence.ErrConflict)
			require.Equal(t, tc.contention, errors.Is(err, persistence.ErrContention))
		})
	}
}
