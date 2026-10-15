// Package database provides functions to migrate the database.
package database

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"

	"github.com/stacklok/toolhive-registry-server/internal/db/exposuretarget"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

//go:embed prime.sql.tmpl
var primeTemplateFS embed.FS

// GetMigrate creates a new migrate instance using the embedded migrations
func GetMigrate(connString string) (*migrate.Migrate, error) {
	return GetMigrateContext(context.Background(), connString)
}

// GetMigrateContext binds acquisition and all driver operations to ctx.
func GetMigrateContext(ctx context.Context, connString string) (*migrate.Migrate, error) {
	return getMigrateContext(ctx, connString, nil)
}

func getMigrateContext(ctx context.Context, connString string, control *pgx.Conn) (*migrate.Migrate, error) {
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

	driver, err := newMigrationDriver(ctx, connString, control)
	if err != nil {
		_ = sourceDriver.Close()
		return nil, fmt.Errorf("failed to create migrate instance: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", sourceDriver, "postgres", driver)
	if err != nil {
		_ = driver.Close()
		_ = sourceDriver.Close()
		return nil, fmt.Errorf("failed to create migrate instance: %w", err)
	}

	return m, nil
}

// MigrateUp executes all pending database migrations
func MigrateUp(ctx context.Context, db *pgx.Conn) error {
	return MigrateUpTo(ctx, db, 30)
}

// MigrateUpTo applies only forward migrations. Cleanup eligibility is checked at
// clean schema 29 before golang-migrate can mark 30 dirty; SQL repeats the guard.
func MigrateUpTo(ctx context.Context, db *pgx.Conn, target uint) (result error) {
	// The owned migration session holds the whole-workflow lock until it exits.
	connString := db.Config().ConnString()
	m, err := getMigrateContext(ctx, connString, db)
	if err != nil {
		return err
	}
	defer func() {
		sourceErr, driverErr := m.Close()
		result = errors.Join(result, sourceErr, driverErr)
	}()

	version, err := cleanMigrationVersion(m)
	if err != nil {
		return err
	}
	if target < version || target == 0 || target > 30 {
		return fmt.Errorf("up requires a supported target at or above existing version %d", version)
	}
	if version < 29 && target >= 29 {
		if err := migrateToVersion(m, 29); err != nil {
			return fmt.Errorf("failed to migrate to exposure review schema 29: %w", err)
		}
		version = 29
	}
	if version == 29 && target == 30 {
		if err := claimsCleanupEligibility(ctx, db); err != nil {
			return err
		}
	}
	if err := migrateToVersion(m, target); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

func lockSchemaMigration(ctx context.Context, conn *pgx.Conn) error {
	// The executing backend owns this lock: even a lost control connection
	// cannot release it while migration SQL is still running on the server.
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(915,30)`); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("schema migration lock unavailable")
	}
	return nil
}

func cleanMigrationVersion(m *migrate.Migrate) (uint, error) {
	version, dirty, err := m.Version()
	if err != nil && err != migrate.ErrNilVersion {
		return 0, fmt.Errorf("cannot inspect migration version: %w", err)
	}
	if dirty {
		return 0, fmt.Errorf("schema version %d is dirty; recover offline before migration", version)
	}
	return version, nil
}

func migrateToVersion(m *migrate.Migrate, target uint) error {
	err := m.Migrate(target)
	if err == migrate.ErrNoChange {
		return nil
	}
	return err
}

// claimsCleanupEligibility shares the authoritative SQL guard without applying
// DDL. A writer racing after rollback is still rejected by the migration itself.
func claimsCleanupEligibility(ctx context.Context, conn *pgx.Conn) error {
	data, err := migrationsFS.ReadFile("migrations/000030_claims_schema_cleanup.up.sql")
	if err != nil {
		return fmt.Errorf("cleanup guard unavailable")
	}
	guard, _, _ := strings.Cut(string(data), "-- End eligibility:")
	guard = strings.Replace(guard, "BEGIN;", "", 1)
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("cannot begin cleanup eligibility inspection")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	locks, check, _ := strings.Cut(guard, "DO $$")
	if _, err := tx.Exec(ctx, locks); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("cleanup eligibility locks unavailable")
	}
	if err := exposuretarget.Verify(ctx, tx); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	if err := exposuretarget.SetContext(ctx, tx); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("cleanup target context unavailable")
	}
	if _, err := tx.Exec(ctx, "DO $$"+check); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("schema cleanup refused at clean version 29; complete offline inventory, acknowledgement, " +
			"retirement and migrate exposure finalize --config <final-config> --confirm-offline; " +
			"see docs/registry-exposure-migration.md")
	}
	return nil
}

// MigrateDown reverts the last migration
func MigrateDown(ctx context.Context, db *pgx.Conn, steps int) (result error) {
	// The owned migration session holds the whole-workflow lock until it exits.
	connString := db.Config().ConnString()
	m, err := getMigrateContext(ctx, connString, db)
	if err != nil {
		return err
	}
	defer func() {
		sourceErr, driverErr := m.Close()
		result = errors.Join(result, sourceErr, driverErr)
	}()

	version, err := cleanMigrationVersion(m)
	if err != nil {
		return err
	}
	if version >= 30 {
		return fmt.Errorf("downgrade unsupported for dirty or claims-free schemas; restore a verified offline backup")
	}
	// Revert migrations
	if err := m.Steps(-steps); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to revert migrations: %w", err)
	}

	return nil
}

// GetVersion returns the current migration version
func GetVersion(connString string) (uint, bool, error) {
	return GetVersionContext(context.Background(), connString)
}

// GetVersionContext inspects the version with cancellable acquisition and queries.
func GetVersionContext(ctx context.Context, connString string) (uint, bool, error) {
	m, err := GetMigrateContext(ctx, connString)
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
