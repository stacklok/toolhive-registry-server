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
// can run it on every start. It reports true only when the rebuild ran over
// existing versions: clearing the mark of a fresh, empty install reports false.
// It borrows db and neither migrates nor closes it.
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
// ones find nothing to do. It reports whether it rebuilt pointers for existing
// versions. Use read committed, or serializable with retry on serialization
// failure: a serializable snapshot taken before the lock makes a conflicting
// pointer write fail instead of overwriting a newer pointer.
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
	names, err := rebuildLatestVersions(ctx, tx)
	return names > 0, err
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
	_, err := rebuildLatestVersions(ctx, tx)
	return err
}

// rebuildLatestVersions picks each (source, kind, name)'s highest version in
// Go, with the shared total order, and applies all of them in a constant
// number of round trips: one scan, one COPY into a temporary table, one
// upsert, one prune. One statement per name took minutes on large remote
// catalogs, longer than the liveness probe allows. It returns the number of
// names that have versions.
func rebuildLatestVersions(ctx context.Context, tx pgx.Tx) (int, error) {
	q := sqlc.New(tx)
	rows, err := q.RepairListVersions(ctx)
	if err != nil {
		return 0, fmt.Errorf("list versions for latest reconciliation: %w", err)
	}
	var winners [][]any
	for i := 0; i < len(rows); {
		best := rows[i]
		j := i + 1
		for j < len(rows) && rows[j].SourceID == best.SourceID && rows[j].EntryType == best.EntryType && rows[j].Name == best.Name {
			if model.CompareVersions(rows[j].Version, best.Version) > 0 {
				best = rows[j]
			}
			j++
		}
		winners = append(winners, []any{best.SourceID, string(best.EntryType), best.Name, best.Version, best.VersionID})
		i = j
	}
	if len(winners) > 0 {
		if err = q.RepairCreateTempLatest(ctx); err != nil {
			return 0, fmt.Errorf("create latest repair table: %w", err)
		}
		copied, err := tx.CopyFrom(ctx, pgx.Identifier{"temp_latest_repair"},
			[]string{"source_id", "entry_type", "name", "version", "version_id"}, pgx.CopyFromRows(winners))
		if err != nil {
			return 0, fmt.Errorf("copy latest pointers: %w", err)
		}
		if int(copied) != len(winners) {
			return 0, fmt.Errorf("copy latest pointers: copied %d of %d", copied, len(winners))
		}
		if err = q.RepairUpsertLatestFromTemp(ctx); err != nil {
			return 0, fmt.Errorf("repair latest pointers: %w", err)
		}
	}
	if err = q.RepairPruneLatest(ctx); err != nil {
		return 0, fmt.Errorf("prune stale latest pointers: %w", err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM latest_entry_version_repair`); err != nil {
		return 0, fmt.Errorf("clear latest repair mark: %w", err)
	}
	return len(winners), nil
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
