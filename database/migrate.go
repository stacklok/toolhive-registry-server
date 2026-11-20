// Package database provides functions to migrate the database.
package database

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
	// postgres driver is required for golang-migrate to work with postgres databases
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

//go:embed prime.sql.tmpl
var primeTemplateFS embed.FS

// LatestSchemaVersion is the newest embedded migration version.
const LatestSchemaVersion uint = 28

// GetMigrate creates a new migrate instance using the embedded migrations.
// It bypasses the migration preconditions; prefer MigrateUp, MigrateUpTo and
// MigrateDown.
func GetMigrate(connString string) (*migrate.Migrate, error) {
	sourceDriver, err := migrationSource()
	if err != nil {
		return nil, err
	}

	// Create migrate instance
	m, err := migrate.NewWithSourceInstance("iofs", sourceDriver, connString)
	if err != nil {
		return nil, fmt.Errorf("failed to create migrate instance: %w", err)
	}

	return m, nil
}

// MigrateUp executes all pending database migrations. It first checks the
// migration preconditions and returns an error wrapping ErrPrecondition,
// without changing the database, when one fails.
func MigrateUp(ctx context.Context, db *pgx.Conn) error {
	m, err := GetMigrate(db.Config().ConnString())
	if err != nil {
		return err
	}
	defer m.Close()

	from, err := cleanVersion(m)
	if err != nil {
		return err
	}
	if err := checkPreconditions(ctx, db, from, LatestSchemaVersion); err != nil {
		return err
	}
	// Up, not Migrate(LatestSchemaVersion): a database already newer than this
	// binary must never be migrated down.
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	return nil
}

// MigrateUpTo moves the schema to target, after the same precondition checks
// as MigrateUp for the versions it crosses.
func MigrateUpTo(ctx context.Context, db *pgx.Conn, target uint) error {
	m, err := GetMigrate(db.Config().ConnString())
	if err != nil {
		return err
	}
	defer m.Close()

	from, err := cleanVersion(m)
	if err != nil {
		return err
	}
	if err := checkPreconditions(ctx, db, from, target); err != nil {
		return err
	}
	if err := m.Migrate(target); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed to run migrations to version %d: %w", target, err)
	}
	return nil
}

// MigrateDown reverts the last steps migrations, after checking the migration
// preconditions for the versions it would cross.
func MigrateDown(ctx context.Context, db *pgx.Conn, steps int) error {
	m, err := GetMigrate(db.Config().ConnString())
	if err != nil {
		return err
	}
	defer m.Close()

	from, err := cleanVersion(m)
	if err != nil {
		return err
	}
	to, err := versionBelow(from, steps)
	if err != nil {
		return err
	}
	if err := checkPreconditions(ctx, db, from, to); err != nil {
		return err
	}
	if err := m.Steps(-steps); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed to revert migrations: %w", err)
	}
	return nil
}

// Force records version as the current schema version and clears the dirty
// flag without running any migration; -1 records no version. It is the manual
// recovery for a migration that failed part-way: the operator must first make
// the schema match that version by hand.
func Force(connString string, version int) error {
	if version < -1 {
		return fmt.Errorf("invalid schema version %d", version)
	}
	m, err := GetMigrate(connString)
	if err != nil {
		return err
	}
	defer m.Close()
	if err := m.Force(version); err != nil {
		return fmt.Errorf("failed to force schema version %d: %w", version, err)
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
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, fmt.Errorf("failed to get version: %w", err)
	}

	return version, dirty, nil
}

// GetPrimeTemplate returns the contents of the prime.sql.tmpl template file
func GetPrimeTemplate() ([]byte, error) {
	return primeTemplateFS.ReadFile("prime.sql.tmpl")
}

func migrationSource() (source.Driver, error) {
	migrationsDir, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to get migrations directory: %w", err)
	}
	sourceDriver, err := iofs.New(migrationsDir, ".")
	if err != nil {
		return nil, fmt.Errorf("failed to create iofs source driver: %w", err)
	}
	return sourceDriver, nil
}

// cleanVersion returns the recorded schema version (0 for an empty database)
// and refuses a dirty one with a pointer to the recovery procedure.
func cleanVersion(m *migrate.Migrate) (uint, error) {
	version, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get version: %w", err)
	}
	if dirty {
		return 0, fmt.Errorf("schema version %d is dirty because a migration failed part-way; make the schema "+
			"match a known version and record it with `thv-registry-api migrate force`, as described in "+
			"docs/database.md (\"Recovering from a dirty schema version\"): %w",
			version, migrate.ErrDirty{Version: int(version)}) //nolint:gosec // migration versions are small
	}
	return version, nil
}

// versionBelow returns the schema version left after reverting steps
// migrations from version from; 0 means none remain.
func versionBelow(from uint, steps int) (uint, error) {
	if steps < 0 {
		return 0, fmt.Errorf("invalid number of steps %d", steps)
	}
	src, err := migrationSource()
	if err != nil {
		return 0, err
	}
	defer src.Close()
	version := from
	for i := 0; i < steps && version > 0; i++ {
		prev, err := src.Prev(version)
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		if err != nil {
			return 0, fmt.Errorf("failed to read migrations: %w", err)
		}
		version = prev
	}
	return version, nil
}
