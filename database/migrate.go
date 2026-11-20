// Package database provides functions to migrate the database.
package database

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
	migratedb "github.com/golang-migrate/migrate/v4/database"
	// The postgres driver also registers the postgres:// scheme GetMigrate uses.
	"github.com/golang-migrate/migrate/v4/database/postgres"
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
	m, driver, err := openMigrate(db.Config().ConnString())
	if err != nil {
		return err
	}
	defer m.Close()

	if err := prepareMigration(ctx, db, driver, func(uint) (uint, error) { return LatestSchemaVersion, nil }); err != nil {
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
	m, driver, err := openMigrate(db.Config().ConnString())
	if err != nil {
		return err
	}
	defer m.Close()

	if err := prepareMigration(ctx, db, driver, func(uint) (uint, error) { return target, nil }); err != nil {
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
	m, driver, err := openMigrate(db.Config().ConnString())
	if err != nil {
		return err
	}
	defer m.Close()

	if err := prepareMigration(ctx, db, driver, func(from uint) (uint, error) {
		return versionBelow(from, steps)
	}); err != nil {
		return err
	}
	if err := m.Steps(-steps); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed to revert migrations: %w", err)
	}
	return nil
}

// openMigrate is GetMigrate that also returns the database driver, so the
// caller can hold golang-migrate's migration lock. Closing the Migrate closes
// the driver.
func openMigrate(connString string) (*migrate.Migrate, migratedb.Driver, error) {
	src, err := migrationSource()
	if err != nil {
		return nil, nil, err
	}
	driver, err := (&postgres.Postgres{}).Open(connString)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open the migration database: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
	if err != nil {
		_ = driver.Close()
		return nil, nil, fmt.Errorf("failed to create migrate instance: %w", err)
	}
	return m, driver, nil
}

// prepareMigration reads the current schema version and checks the
// preconditions for moving to target(version) while holding golang-migrate's
// migration lock, then releases it; the migration takes the lock again. A
// replica migrating at the same time holds that lock and marks the version
// dirty while it runs, so this waits for it instead of reporting the
// in-progress version as a migration that failed part-way.
func prepareMigration(
	ctx context.Context, db *pgx.Conn, driver migratedb.Driver, target func(from uint) (uint, error),
) error {
	if err := driver.Lock(); err != nil {
		return fmt.Errorf("failed to acquire the migration lock: %w", err)
	}
	defer func() { _ = driver.Unlock() }()
	from, err := cleanVersion(driver)
	if err != nil {
		return err
	}
	to, err := target(from)
	if err != nil {
		return err
	}
	return runPreconditions(ctx, db, from, to)
}

// runPreconditions runs the migration preconditions on a connection of their
// own, in a read-only transaction that ends before any migration runs. The
// caller's connection may be inside a transaction (serve wraps MigrateUp in
// one, as v1.5.4 did); reads there would keep locks on tables the migrations
// alter until the caller commits, so a migration would wait on its own caller
// forever.
func runPreconditions(ctx context.Context, db *pgx.Conn, from, to uint) error {
	conn, err := pgx.ConnectConfig(ctx, db.Config().Copy())
	if err != nil {
		return fmt.Errorf("failed to connect to check migration preconditions: %w", err)
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("failed to begin precondition checks: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := checkPreconditions(ctx, tx, from, to); err != nil {
		return err
	}
	return tx.Commit(ctx)
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
func cleanVersion(driver migratedb.Driver) (uint, error) {
	version, dirty, err := driver.Version()
	if err != nil {
		return 0, fmt.Errorf("failed to get version: %w", err)
	}
	if version == migratedb.NilVersion {
		return 0, nil
	}
	if dirty {
		return 0, fmt.Errorf("schema version %d is dirty because a migration failed part-way; make the schema "+
			"match a known version and record it with `thv-registry-api migrate force`, as described in "+
			"docs/database.md (\"Recovering from a dirty schema version\"): %w",
			version, migrate.ErrDirty{Version: version})
	}
	if version < 0 {
		return 0, fmt.Errorf("invalid schema version %d", version)
	}
	return uint(version), nil
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
