package database

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// MigrationsFS exposes the embedded migrations to the external test package.
var MigrationsFS = migrationsFS

// CheckBeforeMigrating opens golang-migrate for db, closes connected, and
// once ready is closed reads the schema version and checks the preconditions
// for MigrateUp, as MigrateUp does before migrating. The gap lets a test start
// another replica's migration between the two steps.
func CheckBeforeMigrating(ctx context.Context, db *pgx.Conn, connected chan<- struct{}, ready <-chan struct{}) error {
	m, driver, err := openMigrate(db.Config().ConnString())
	close(connected)
	if err != nil {
		return err
	}
	defer m.Close()
	<-ready
	return prepareMigration(ctx, db, driver, func(uint) (uint, error) { return LatestSchemaVersion, nil })
}
