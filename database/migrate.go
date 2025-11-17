// Package database provides functions to migrate the database.
// This is a temporary hack while we wait for the tooling of choice
// to be on main.
package database

import (
	"context"
	_ "embed"

	"github.com/jackc/pgx/v5"
)

//go:embed migrations/000001_init.up.sql
var initMigrationUp string

//go:embed migrations/000001_init.down.sql
var initMigrationDown string

// MigrateUp executes the database migrations
func MigrateUp(ctx context.Context, db *pgx.Conn) error {
	_, err := db.Exec(ctx, initMigrationUp)
	return err
}

// MigrateDown executes the database migrations in reverse order
func MigrateDown(ctx context.Context, db *pgx.Conn) error {
	_, err := db.Exec(ctx, initMigrationDown)
	return err
}
