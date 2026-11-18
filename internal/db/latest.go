package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
)

// TxBeginner is satisfied by *pgx.Conn and *pgxpool.Pool.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// RepairPendingLatestVersions rebuilds every latest pointer once, when
// migration 25 has marked the database for repair, and clears the mark in the
// same transaction. Once the mark is gone it only reads one row, so callers
// can run it on every start. It borrows db and neither migrates nor closes it.
func RepairPendingLatestVersions(ctx context.Context, db TxBeginner) (bool, error) {
	pending, err := LatestRepairPending(ctx, db)
	if err != nil || !pending {
		return false, err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin latest repair: %w", err)
	}
	defer tx.Rollback(context.Background())
	repaired, err := RepairPendingLatestVersionsTx(ctx, tx)
	if err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit latest repair: %w", err)
	}
	return repaired, nil
}

// LatestRepairPending reports, without locking, whether the repair mark is set.
func LatestRepairPending(ctx context.Context, db TxBeginner) (bool, error) {
	var pending bool
	if err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM latest_entry_version_repair)`).Scan(&pending); err != nil {
		return false, fmt.Errorf("check latest repair mark: %w", err)
	}
	return pending, nil
}

// RepairPendingLatestVersionsTx takes the write locks, then repairs only if
// the mark is still set: concurrent starts serialize on the lock and the later
// ones find nothing to do. Use read committed, or serializable with retry on
// serialization failure: a serializable snapshot taken before the lock makes a
// conflicting pointer write fail instead of overwriting a newer pointer.
func RepairPendingLatestVersionsTx(ctx context.Context, tx pgx.Tx) (bool, error) {
	if err := lockLatestTables(ctx, tx); err != nil {
		return false, err
	}
	var pending bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM latest_entry_version_repair)`).Scan(&pending); err != nil {
		return false, fmt.Errorf("check latest repair mark: %w", err)
	}
	if !pending {
		return false, nil
	}
	return true, ReconcileLatestVersionsTx(ctx, tx)
}

// ReconcileLatestVersions rebuilds every pointer unconditionally, as an
// explicit operation, and clears the repair mark.
func ReconcileLatestVersions(ctx context.Context, db TxBeginner) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin latest reconciliation: %w", err)
	}
	defer tx.Rollback(context.Background())
	if err = lockLatestTables(ctx, tx); err != nil {
		return err
	}
	if err = ReconcileLatestVersionsTx(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ReconcileLatestVersionsTx scans and repairs within a caller-held write lock
// and clears the repair mark.
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
	if _, err = tx.Exec(ctx, `DELETE FROM latest_entry_version_repair`); err != nil {
		return fmt.Errorf("clear latest repair mark: %w", err)
	}
	return nil
}

// lockLatestTables excludes earlier and later writers before the scan.
func lockLatestTables(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '5s'`); err != nil {
		return fmt.Errorf("set latest repair lock timeout: %w", err)
	}
	if _, err := tx.Exec(ctx, `LOCK TABLE source, registry_entry, entry_version, latest_entry_version
 IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return fmt.Errorf("lock latest repair tables: %w", err)
	}
	return nil
}
