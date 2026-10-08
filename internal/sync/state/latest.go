package state

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
)

type latestCandidate struct {
	id      uuid.UUID
	version string
}

type latestGroup struct {
	source          uuid.UUID
	name            string
	previous        string
	previousVersion string
	best            map[string]latestCandidate
	kindByID        map[string]string
}

// ReconcileLatestVersions repairs persisted latest pointers before services or HTTP
// listeners are constructed. The caller retains ownership of the pool.
func ReconcileLatestVersions(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin latest reconciliation: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is harmless

	// Take the locks before the first read: a writer that started earlier must
	// finish before the scan, and a later publish/delete must wait until commit.
	// The timeout fails startup rather than waiting indefinitely for an old writer.
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '5s'`); err != nil {
		return fmt.Errorf("set latest reconciliation lock timeout: %w", err)
	}
	if _, err := tx.Exec(ctx, `LOCK TABLE source, registry_entry, entry_version, latest_entry_version
		IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return fmt.Errorf("lock latest reconciliation tables: %w", err)
	}
	if err := reconcileLatestVersions(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// reconcileLatestVersions corrects previously stored pointers after a change to
// version ordering. The table is keyed by source and name (not kind): retain
// the existing kind for legacy cross-kind name collisions.
//
//nolint:gocyclo // scan and repair need to coordinate errors and legacy cross-kind pointers
func reconcileLatestVersions(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, `SELECT e.source_id, e.name, e.entry_type::text, v.id, v.version,
		coalesce(l.latest_version_id::text, ''), coalesce(l.version, '')
		FROM registry_entry e JOIN entry_version v ON v.entry_id=e.id
		LEFT JOIN latest_entry_version l ON l.source_id=e.source_id AND l.name=e.name
		ORDER BY e.source_id, e.name, e.entry_type, v.version`)
	if err != nil {
		return fmt.Errorf("list versions for latest reconciliation: %w", err)
	}
	var groups []*latestGroup
	for rows.Next() {
		var source, id uuid.UUID
		var name, kind, version, previous, previousVersion string
		if err = rows.Scan(&source, &name, &kind, &id, &version, &previous, &previousVersion); err != nil {
			break
		}
		var group *latestGroup
		if len(groups) > 0 {
			group = groups[len(groups)-1]
		}
		if group == nil || group.source != source || group.name != name {
			group = &latestGroup{source: source, name: name, previous: previous, previousVersion: previousVersion,
				best: make(map[string]latestCandidate), kindByID: make(map[string]string)}
			groups = append(groups, group)
		}
		group.kindByID[id.String()] = kind
		if candidate, ok := group.best[kind]; !ok || model.CompareVersions(version, candidate.version) > 0 {
			group.best[kind] = latestCandidate{id: id, version: version}
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return fmt.Errorf("read versions for latest reconciliation: %w", err)
	}
	for _, group := range groups {
		kind := group.kindByID[group.previous]
		if kind == "" {
			if len(group.best) != 1 {
				slog.Warn("Skipping ambiguous legacy latest pointer without a selected kind",
					"source_id", group.source, "name", group.name)
				continue
			}
			for k := range group.best {
				kind = k
			}
		}
		candidate := group.best[kind]
		if group.previous == candidate.id.String() && group.previousVersion == candidate.version {
			continue
		}
		_, err := tx.Exec(ctx, `INSERT INTO latest_entry_version (source_id, name, version, latest_version_id)
			VALUES ($1, $2, $3, $4) ON CONFLICT (source_id, name) DO UPDATE
			SET version=excluded.version, latest_version_id=excluded.latest_version_id`,
			group.source, group.name, candidate.version, candidate.id)
		if err != nil {
			return fmt.Errorf("reconcile latest for source %s name %q: %w", group.source, group.name, err)
		}
	}
	return nil
}
