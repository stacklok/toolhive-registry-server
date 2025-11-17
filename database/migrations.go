// Package database provides database migration tooling.
package database

import (
	"embed"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var fs embed.FS

// migrationsFromSource returns a migration source driver from the embedded migrations.
func migrationsFromSource() source.Driver {
	d, err := iofs.New(fs, "migrations")
	if err != nil {
		panic(err)
	}
	return d
}

// Migrator is the interface for the migration tooling.
type Migrator interface {
	Up() error
	Down() error
	Steps(int) error
	Version() (uint, bool, error)
}

// NewFromConnectionString returns a new migration instance from the given connection string.
func NewFromConnectionString(connString string) (Migrator, error) {
	d := migrationsFromSource()
	return migrate.NewWithSourceInstance("iofs", d, connString)
}
