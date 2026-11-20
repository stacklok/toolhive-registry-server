package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrPrecondition is wrapped by the error a migration run returns when it is
// refused before anything is applied. The database is left exactly as it was:
// same version, not dirty.
var ErrPrecondition = errors.New("migration precondition failed")

// queryer is the read access a precondition needs; *pgx.Conn and pgx.Tx satisfy it.
type queryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// precondition is a read-only check that must hold before a migration run
// moves the schema from one version to another. Checks run before
// golang-migrate takes its lock or applies anything, so a refused run leaves
// the database untouched. To guard another migration, append an entry to
// preconditions.
type precondition struct {
	// applies reports whether a run from version from to version to crosses
	// the migration this check guards.
	applies func(from, to uint) bool
	// check inspects the database at schema version from. It must not write.
	check func(ctx context.Context, q queryer, from uint) error
}

// preconditions lists every check, in the order they run.
var preconditions = []precondition{
	// Migration 24 adds a unique index allowing one managed source. On a database
	// with two it would fail part-way and leave version 24 dirty, which neither
	// this version, v1.5.4 nor migrate down can recover from.
	{applies: crossesUp(24), check: checkSingleManagedSource},
	// Migration 26's down step refuses while a sync lease is live, but only after
	// the later down steps already ran, leaving the database dirty one version
	// below its real schema.
	{applies: crossesDown(26), check: checkNoLiveSyncLease},
	// Reverting migration 29 drops the recorded authorization mode, and
	// re-applying it stamps any catalog as claims mode. A Cedar deployment has no
	// earlier version to roll back to, and a later upgrade would refuse its
	// Cedar configuration.
	{applies: crossesDown(29), check: checkNotCedarMode},
}

// crossesUp matches an upgrade that applies migration v.
func crossesUp(v uint) func(from, to uint) bool {
	return func(from, to uint) bool { return from < v && to >= v }
}

// crossesDown matches a downgrade that reverts migration v.
func crossesDown(v uint) func(from, to uint) bool {
	return func(from, to uint) bool { return from >= v && to < v }
}

// checkPreconditions runs every check that applies to moving from one schema
// version to another and returns the first failure.
func checkPreconditions(ctx context.Context, q queryer, from, to uint) error {
	for _, p := range preconditions {
		if !p.applies(from, to) {
			continue
		}
		if err := p.check(ctx, q, from); err != nil {
			return err
		}
	}
	return nil
}

// managedSourceQuery returns the query counting managed sources at schema
// version from: the source table exists from migration 15, the source_type
// column from migration 8, and migration 2 renamed reg_type LOCAL to MANAGED.
func managedSourceQuery(from uint) string {
	const agg = `SELECT count(*), COALESCE(string_agg(name, ', ' ORDER BY name), '') `
	switch {
	case from >= 15:
		return agg + `FROM source WHERE source_type = 'managed'`
	case from >= 8:
		return agg + `FROM registry WHERE COALESCE(source_type,
 CASE WHEN reg_type::text = 'MANAGED' THEN 'managed' END) = 'managed'`
	default:
		return agg + `FROM registry WHERE reg_type::text IN ('LOCAL', 'MANAGED')`
	}
}

func checkSingleManagedSource(ctx context.Context, q queryer, from uint) error {
	if from == 0 {
		return nil // empty database
	}
	var (
		count int64
		names string
	)
	if err := q.QueryRow(ctx, managedSourceQuery(from)).Scan(&count, &names); err != nil {
		return fmt.Errorf("check managed sources before migration 24: %w", err)
	}
	if count <= 1 {
		return nil
	}
	return fmt.Errorf("%w: the database has %d managed sources (%s) but this version allows only one; "+
		"merge or remove the extra managed sources before upgrading, as described in docs/database.md "+
		"(\"Repairing more than one managed source\"); nothing was changed",
		ErrPrecondition, count, names)
}

func checkNoLiveSyncLease(ctx context.Context, q queryer, _ uint) error {
	var names string
	err := q.QueryRow(ctx, `SELECT COALESCE(string_agg(s.name, ', ' ORDER BY s.name), '')
FROM registry_sync rs JOIN source s ON s.id = rs.source_id
WHERE rs.lease_id IS NOT NULL AND rs.lease_expires_at > clock_timestamp()`).Scan(&names)
	if err != nil {
		return fmt.Errorf("check sync leases before reverting migration 26: %w", err)
	}
	if names == "" {
		return nil
	}
	return fmt.Errorf("%w: sync jobs hold live leases on sources %s; stop every server, wait for the leases "+
		"to expire, then retry the downgrade; nothing was changed", ErrPrecondition, names)
}

func checkNotCedarMode(ctx context.Context, q queryer, _ uint) error {
	var cedar bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM authorization_mode WHERE mode = 'cedar')`).Scan(&cedar)
	if err != nil {
		return fmt.Errorf("check the authorization mode before reverting migration 29: %w", err)
	}
	if !cedar {
		return nil
	}
	return fmt.Errorf("%w: this database runs in Cedar authorization mode, which no version before migration 29 "+
		"supports, and reverting it would record claims mode on the next upgrade; nothing was changed",
		ErrPrecondition)
}
