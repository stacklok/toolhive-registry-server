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

	// The case the cap actually exists for: the OTel Prometheus exporter builds
	// its own context.TODO() rather than propagating the request context, so a
	// deadline-free caller is the norm on the scrape path. Without the cap the
	// query would be unbounded. Asserted via the deadline the query observes,
	// since a deadline-free caller can't otherwise distinguish capped from
	// uncapped on a fast query.
	t.Run("applies its own deadline to a caller that has none", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		_, hasDeadline := ctx.Deadline()
		require.False(t, hasDeadline, "precondition: caller supplies no deadline")

		queryCtx, cancelQuery := registryMetricCountsContext(ctx)
		defer cancelQuery()

		deadline, ok := queryCtx.Deadline()
		require.True(t, ok, "expected the reader to impose a deadline of its own")
		require.LessOrEqual(t, time.Until(deadline), registryMetricCountsQueryTimeout)
	})

	// A caller that already has a tighter deadline keeps it — the cap is an
	// upper bound, not a floor.
	t.Run("does not extend a caller's tighter deadline", func(t *testing.T) {
		t.Parallel()

		tight := 50 * time.Millisecond
		ctx, cancel := context.WithTimeout(context.Background(), tight)
		defer cancel()

		queryCtx, cancelQuery := registryMetricCountsContext(ctx)
		defer cancelQuery()

		deadline, ok := queryCtx.Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), tight)
	})
}
