package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
)

// ReconcileLatestVersions rebuilds every kind/name pointer before readers are
// served. It borrows the caller's pool and does not migrate or close it.
func ReconcileLatestVersions(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin latest reconciliation: %w", err)
	}
	defer tx.Rollback(context.Background())
	// Take table locks before scanning to exclude earlier and later writers.
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout = '5s'`); err != nil {
		return fmt.Errorf("set reconciliation timeout: %w", err)
	}
	if _, err = tx.Exec(ctx, `LOCK TABLE source, registry_entry, entry_version, latest_entry_version
 IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return fmt.Errorf("lock latest reconciliation tables: %w", err)
	}
	if err = ReconcileLatestVersionsTx(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ReconcileLatestVersionsTx scans and repairs within a caller-held write lock.
// It is used by the standalone startup and by migration regression tests.
func ReconcileLatestVersionsTx(ctx context.Context, tx pgx.Tx) error {
	q := sqlc.New(tx)
	rows, err := q.RepairListVersions(ctx)
	if err != nil {
		return fmt.Errorf("list versions for latest reconciliation: %w", err)
	}
	for i := 0; i < len(rows); {
		best := rows[i]
		j := i + 1
		for j < len(rows) && rows[j].SourceID == best.SourceID && rows[j].EntryType == best.EntryType && rows[j].Name == best.Name {
			if model.CompareVersions(rows[j].Version, best.Version) > 0 {
				best = rows[j]
			}
			j++
		}
		if err = q.RepairSetLatest(ctx, sqlc.RepairSetLatestParams(best)); err != nil {
			return fmt.Errorf("repair latest pointer: %w", err)
		}
		i = j
	}
	if err = q.RepairPruneLatest(ctx); err != nil {
		return fmt.Errorf("prune stale latest pointers: %w", err)
	}
	return nil
}
