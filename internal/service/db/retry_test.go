package database

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestRetrySerializable(t *testing.T) {
	t.Parallel()
	conflict := fmt.Errorf("failed to delete source: %w", &pgconn.PgError{Code: "40001"})

	calls := 0
	v, err := retrySerializable(t.Context(), func() (int, error) {
		calls++
		if calls < 3 {
			return 0, conflict
		}
		return 7, nil
	})
	require.NoError(t, err)
	require.Equal(t, 7, v)
	require.Equal(t, 3, calls, "serialization failures are retried")

	calls = 0
	_, err = retrySerializable(t.Context(), func() (int, error) { calls++; return 0, conflict })
	require.ErrorIs(t, err, conflict)
	require.Equal(t, maxWriteAttempts, calls, "retries are bounded")

	calls = 0
	other := errors.New("not found")
	_, err = retrySerializable(t.Context(), func() (int, error) { calls++; return 0, other })
	require.ErrorIs(t, err, other)
	require.Equal(t, 1, calls, "other errors are not retried")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	calls = 0
	_, err = retrySerializable(ctx, func() (int, error) { calls++; return 0, conflict })
	require.ErrorIs(t, err, conflict)
	require.Equal(t, 1, calls, "a cancelled request stops retrying")
}
