package storage

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
)

func TestRegistryMetricsReader_RegistryMetricCounts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, cleanupFunc := database.SetupTestDB(t)
	t.Cleanup(cleanupFunc)

	require.NoError(t, database.MigrateUp(ctx, db))

	pool, err := pgxpool.New(ctx, db.Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	factory := &DatabaseFactory{pool: pool}
	reader, err := factory.CreateRegistryMetricsReader(ctx)
	require.NoError(t, err)

	t.Run("returns empty counts against a fresh database", func(t *testing.T) {
		t.Parallel()

		counts, err := reader.RegistryMetricCounts(context.Background())
		require.NoError(t, err)
		require.Empty(t, counts)
	})

	t.Run("respects an already-expired context deadline", func(t *testing.T) {
		t.Parallel()

		expiredCtx, cancel := context.WithTimeout(context.Background(), 0)
		defer cancel()
		time.Sleep(time.Millisecond)

		_, err := reader.RegistryMetricCounts(expiredCtx)
		require.Error(t, err)
	})
}
