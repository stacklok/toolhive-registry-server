package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
)

// ErrAuthorizationModeMismatch reports that the configured authorization mode
// differs from the one the database was set up with.
var ErrAuthorizationModeMismatch = errors.New("authorization mode mismatch")

// EnsureAuthorizationMode records mode on a database that has no mode yet and
// fails when the database already uses a different one. A deployment keeps
// one mode for its whole life; using the other mode needs a new database.
func EnsureAuthorizationMode(ctx context.Context, db TxBeginner, mode string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin authorization mode check: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	q := sqlc.New(tx)
	if err := q.RecordAuthorizationMode(ctx, mode); err != nil {
		return fmt.Errorf("record authorization mode: %w", err)
	}
	recorded, err := q.GetAuthorizationMode(ctx)
	if err != nil {
		return fmt.Errorf("read authorization mode: %w", err)
	}
	if recorded != mode {
		return fmt.Errorf("%w: this database uses %s mode but the configuration selects %s mode; "+
			"an existing deployment cannot switch modes, so start %s mode on a new database",
			ErrAuthorizationModeMismatch, recorded, mode, mode)
	}
	return tx.Commit(ctx)
}
