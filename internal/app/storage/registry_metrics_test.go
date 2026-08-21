package storage

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/internal/telemetry"
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

// The caching reader in internal/telemetry stamps a successful entry's expiry
// from *before* the query runs, which only works while the query is guaranteed
// to finish well inside the cache TTL. That relationship spans two packages —
// storage imports telemetry, never the reverse — so telemetry cannot assert it
// and it has to be checked here, where both values are visible. Without these
// checks, raising registryMetricCountsQueryTimeout above the TTL disables the
// cache silently: every caller re-runs the full-table aggregate, on an
// unauthenticated /metrics endpoint, with no test going red.
func TestRegistryMetricCountsTimeoutInvariants(t *testing.T) {
	t.Parallel()

	t.Run("query timeout is below the caching reader's success TTL", func(t *testing.T) {
		t.Parallel()

		require.Less(t, registryMetricCountsQueryTimeout, telemetry.RegistryMetricCountsCacheTTL,
			"a query timeout at or above the success TTL writes cache entries that expired "+
				"before they were written, so every caller re-runs the full-table aggregate")
	})

	// Being merely below the TTL is not enough. A slow-but-successful query
	// consumes its own duration out of the entry's remaining life, so a
	// timeout close to the TTL means a query that nearly times out yields an
	// entry that is nearly expired on arrival — the cache technically works
	// while bounding almost nothing. Requiring a third of the TTL to survive
	// the slowest permitted query keeps the useful life meaningful.
	//
	// The current values (20s timeout, 30s TTL) sit exactly on this bound, so
	// raising the timeout is a deliberate decision that has to raise the TTL
	// with it rather than something that can drift in unnoticed.
	t.Run("query timeout leaves usable cache life after a maximally slow query", func(t *testing.T) {
		t.Parallel()

		require.LessOrEqual(t, registryMetricCountsQueryTimeout, telemetry.RegistryMetricCountsCacheTTL*2/3,
			"a query timeout this close to the success TTL leaves a slow query's result "+
				"nearly expired on arrival, so the cache bounds almost nothing")
	})

	// The other direction, for the failure path. The caching reader stamps its
	// negative TTL from *after* the query precisely because a failing read
	// most often fails by exhausting this timeout; if the timeout were the
	// shorter of the two, that reasoning — and the comment recording it —
	// would no longer hold.
	t.Run("query timeout is above the caching reader's negative TTL", func(t *testing.T) {
		t.Parallel()

		require.Greater(t, registryMetricCountsQueryTimeout, telemetry.RegistryMetricCountsErrorCacheTTL,
			"the negative TTL assumes a failing read outlasts it; a shorter query timeout "+
				"inverts that and invalidates the reader's expiry stamping")
	})
}
