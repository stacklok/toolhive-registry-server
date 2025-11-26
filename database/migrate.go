// Package database provides functions to migrate the database.
package database

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
	// postgres driver is required for golang-migrate to work with postgres databases
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

//go:embed prime.sql.tmpl
var primeTemplateFS embed.FS

// GetMigrate creates a new migrate instance using the embedded migrations
func GetMigrate(connString string) (*migrate.Migrate, error) {
	// Get the migrations subdirectory from the embedded filesystem
	migrationsDir, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to get migrations directory: %w", err)
	}

	// Create the iofs source driver
	sourceDriver, err := iofs.New(migrationsDir, ".")
	if err != nil {
		return nil, fmt.Errorf("failed to create iofs source driver: %w", err)
	}

	// Create migrate instance
	m, err := migrate.NewWithSourceInstance("iofs", sourceDriver, connString)
	if err != nil {
		return nil, fmt.Errorf("failed to create migrate instance: %w", err)
	}

	return m, nil
}

// MigrateUp executes all pending database migrations
func MigrateUp(_ context.Context, db *pgx.Conn) error {
	// Get connection string from the connection
	connString := db.Config().ConnString()

	// Create migrate instance
	m, err := GetMigrate(connString)
	if err != nil {
		return err
	}
	defer m.Close()

	// Run migrations
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

// MigrateDown reverts the last migration
func MigrateDown(_ context.Context, db *pgx.Conn, steps int) error {
	// Get connection string from the connection
	connString := db.Config().ConnString()

	// Create migrate instance
	m, err := GetMigrate(connString)
	if err != nil {
		return err
	}
	defer m.Close()

	// Revert migrations
	if err := m.Steps(-steps); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to revert migrations: %w", err)
	}

	return nil
}

// GetVersion returns the current migration version
func GetVersion(connString string) (uint, bool, error) {
	m, err := GetMigrate(connString)
	if err != nil {
		return 0, false, err
	}
	defer m.Close()

	version, dirty, err := m.Version()
	if err != nil && err != migrate.ErrNilVersion {
		return 0, false, fmt.Errorf("failed to get version: %w", err)
	}

	return version, dirty, nil
}

// GetPrimeTemplate returns the contents of the prime.sql.tmpl template file
func GetPrimeTemplate() ([]byte, error) {
	return primeTemplateFS.ReadFile("prime.sql.tmpl")
}
