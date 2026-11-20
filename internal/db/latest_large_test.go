package db_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database/dbtest"
	dbcore "github.com/stacklok/toolhive-registry-server/internal/db"
)

// The run-once latest-pointer repair runs before the health server starts, so
// it must finish well inside the chart's ~60 s liveness budget even for a
// large catalog. One round trip per name took 32 s locally (139 s remote) for
// 150k names; the rebuild now uses a constant number of round trips.
func TestLatestRepairLargeCatalog(t *testing.T) {
	t.Parallel()
	names := 150_000
	if testing.Short() {
		names = 5_000
	}
	conn, _ := dbtest.SetupTestDB(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, conn.Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	// Two versions per name, where semver order (10.0.0) and byte order (9.0.0)
	// disagree, and a stale pointer to the older one for half of the names.
	_, err = pool.Exec(ctx, `INSERT INTO source (name, creation_type, source_type, syncable) VALUES ('large', 'API', 'managed', false)`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO registry_entry (source_id, entry_type, name)
 SELECT s.id, 'MCP', 'com.example/server-' || g FROM source s, generate_series(1, $1::int) g WHERE s.name = 'large'`, names)
	require.NoError(t, err)
	for _, seed := range []string{
		`INSERT INTO entry_version (entry_id, name, version)
 SELECT e.id, e.name, v FROM registry_entry e, unnest(ARRAY['9.0.0', '10.0.0']) v`,
		`INSERT INTO latest_entry_version (source_id, entry_type, name, version, latest_version_id)
 SELECT e.source_id, e.entry_type, e.name, v.version, v.id
 FROM registry_entry e JOIN entry_version v ON v.entry_id = e.id
 WHERE v.version = '9.0.0' AND right(e.name, 1) IN ('0', '2', '4', '6', '8')`,
		`INSERT INTO latest_entry_version_repair DEFAULT VALUES ON CONFLICT DO NOTHING`,
	} {
		_, err = pool.Exec(ctx, seed)
		require.NoError(t, err)
	}

	cfg, err := pgxpool.ParseConfig(conn.Config().ConnString())
	require.NoError(t, err)
	statements := &statementCounter{}
	cfg.ConnConfig.Tracer = statements
	traced, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(traced.Close)

	started := time.Now()
	repaired, err := dbcore.RepairPendingLatestVersions(ctx, traced)
	elapsed := time.Since(started)
	require.NoError(t, err)
	require.True(t, repaired, "a repair over existing versions reports that it ran")
	t.Logf("latest-pointer repair of %d names took %s in %d statements", names, elapsed, statements.n.Load())
	// The statement count is the regression guard: the repair used to make one
	// round trip per name. Wall-clock time depends on the machine, so it is
	// only logged above.
	require.Less(t, statements.n.Load(), int64(20), "round trips must not grow with the catalog")

	var total, wrong int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE l.version <> '10.0.0' OR v.version <> '10.0.0')
 FROM latest_entry_version l JOIN entry_version v ON v.id = l.latest_version_id`).Scan(&total, &wrong))
	require.Equal(t, names, total)
	require.Zero(t, wrong)
	pending, err := dbcore.LatestRepairPending(ctx, pool)
	require.NoError(t, err)
	require.False(t, pending)
}

// A fresh install also carries the repair mark from migration 25; clearing it
// over an empty catalog is not a repair worth reporting.
func TestLatestRepairFreshInstallReportsNothing(t *testing.T) {
	t.Parallel()
	conn, _ := dbtest.SetupTestDB(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, conn.Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	pending, err := dbcore.LatestRepairPending(ctx, pool)
	require.NoError(t, err)
	require.True(t, pending, "migration 25 marks every database")
	repaired, err := dbcore.RepairPendingLatestVersions(ctx, pool)
	require.NoError(t, err)
	require.False(t, repaired)
	pending, err = dbcore.LatestRepairPending(ctx, pool)
	require.NoError(t, err)
	require.False(t, pending, "the mark is cleared anyway")
}

// statementCounter counts the statements a pool sends (COPY is not a query).
type statementCounter struct{ n atomic.Int64 }

func (c *statementCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return ctx
}

func (*statementCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
