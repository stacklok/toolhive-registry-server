package database

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

const (
	// DBName is the name of the test database.
	DBName = "testdb"
	// DBUser is the username for the root user of the test database.
	DBUser = "testuser"
	// DBPass is the password for the root user of the test database.
	DBPass = "testpass"
)

// SetupTestDBContainer starts a dedicated embedded Postgres process and returns a connection to the database.
// The name is retained for compatibility with existing tests.
//
//nolint:revive
func SetupTestDBContainer(t *testing.T, ctx context.Context) (*pgx.Conn, func()) {
	t.Helper()

	postgres, cfg, err := startTestPostgres(t.TempDir())
	require.NoError(t, err,
		"start embedded PostgreSQL 16; the first run requires network access to download and cache PostgreSQL binaries")

	var (
		db   *pgx.Conn
		once sync.Once
	)
	cleanup := func() {
		once.Do(func() {
			cleanupCtx := testCleanupContext(t)
			if db != nil {
				if err := db.Close(cleanupCtx); err != nil {
					t.Errorf("close test database connection: %v", err)
				}
			}
			if err := postgres.Stop(); err != nil {
				t.Errorf("stop embedded Postgres: %v", err)
			}
		})
	}
	t.Cleanup(cleanup)

	db, err = pgx.Connect(ctx, testConnectionURL(cfg))
	if err != nil {
		cleanup()
		require.NoError(t, err)
	}

	return db, cleanup
}

// SetupTestDB returns an isolated, migrated database on a package-shared Postgres process.
func SetupTestDB(t *testing.T) (*pgx.Conn, func()) {
	t.Helper()

	db, release, err := acquireSharedTestDB(t.Context())
	require.NoError(t, err)

	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			cleanupCtx := testCleanupContext(t)
			if err := release(cleanupCtx); err != nil {
				t.Errorf("release shared test database: %v", err)
			}
		})
	}
	t.Cleanup(cleanup)
	return db, cleanup
}

func testCleanupContext(t *testing.T) context.Context {
	t.Helper()
	return context.WithoutCancel(t.Context())
}

func startTestPostgres(runtimePath string) (*embeddedpostgres.EmbeddedPostgres, embeddedpostgres.Config, error) {
	// Port(0) lets the library pick a free port and retry on bind conflicts.
	// Binaries are downloaded once into the library's shared cache under
	// os.UserCacheDir(), which it guards with its own file lock.
	cfg := embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.V16).
		Database(DBName).
		Username(DBUser).
		Password(DBPass).
		Port(0).
		RuntimePath(runtimePath).
		Logger(io.Discard).
		StartParameters(map[string]string{
			"fsync":         "off",
			"log_statement": "all",
		})
	postgres := embeddedpostgres.NewDatabase(cfg)
	if err := postgres.Start(); err != nil {
		return nil, embeddedpostgres.Config{}, fmt.Errorf("start embedded PostgreSQL: %w", err)
	}

	// Pin the chosen port so derived configs (other databases on the same
	// server) produce working connection URLs.
	return postgres, cfg.Port(postgres.GetPort()), nil
}

type sharedTestPostgres struct {
	postgres    *embeddedpostgres.EmbeddedPostgres
	config      embeddedpostgres.Config
	runtimePath string
	uses        int
}

var sharedTestDB struct {
	sync.Mutex
	instance *sharedTestPostgres
}

func acquireSharedTestDB(ctx context.Context) (*pgx.Conn, func(context.Context) error, error) {
	sharedTestDB.Lock()
	defer sharedTestDB.Unlock()

	// Database creation and migration are serialized because migrations create
	// cluster-global roles that cannot be created safely by concurrent callers.
	if sharedTestDB.instance == nil {
		instance, err := createSharedTestPostgres()
		if err != nil {
			return nil, nil, err
		}
		sharedTestDB.instance = instance
	}

	instance := sharedTestDB.instance
	databaseName := "testdb_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	adminConfig := instance.config.Database("postgres")
	admin, err := pgx.Connect(ctx, testConnectionURL(adminConfig))
	if err != nil {
		return nil, nil, errors.Join(
			fmt.Errorf("connect to shared test database: %w", err),
			discardUnusedSharedTestPostgres(instance),
		)
	}
	_, createErr := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{databaseName}.Sanitize())
	closeErr := admin.Close(ctx)
	if createErr != nil || closeErr != nil {
		return nil, nil, errors.Join(createErr, closeErr, discardUnusedSharedTestPostgres(instance))
	}

	databaseConfig := instance.config.Database(databaseName)
	db, err := pgx.Connect(ctx, testConnectionURL(databaseConfig))
	if err != nil {
		return nil, nil, errors.Join(
			fmt.Errorf("connect to isolated test database: %w", err),
			dropSharedDatabase(ctx, instance, databaseName),
			discardUnusedSharedTestPostgres(instance),
		)
	}
	if err := MigrateUp(ctx, db); err != nil {
		return nil, nil, errors.Join(
			err,
			db.Close(ctx),
			dropSharedDatabase(ctx, instance, databaseName),
			discardUnusedSharedTestPostgres(instance),
		)
	}
	if err := RegisterEnumArrayCodecs(ctx, db); err != nil {
		return nil, nil, errors.Join(
			err,
			db.Close(ctx),
			dropSharedDatabase(ctx, instance, databaseName),
			discardUnusedSharedTestPostgres(instance),
		)
	}
	instance.uses++

	return db, func(cleanupCtx context.Context) error {
		return releaseSharedTestDB(cleanupCtx, instance, databaseName, db)
	}, nil
}

func dropSharedDatabase(ctx context.Context, instance *sharedTestPostgres, databaseName string) error {
	adminConfig := instance.config.Database("postgres")
	admin, err := pgx.Connect(ctx, testConnectionURL(adminConfig))
	if err != nil {
		return err
	}
	_, dropErr := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{databaseName}.Sanitize())
	return errors.Join(dropErr, admin.Close(ctx))
}

func discardUnusedSharedTestPostgres(instance *sharedTestPostgres) error {
	if instance.uses != 0 {
		return nil
	}
	sharedTestDB.instance = nil
	return errors.Join(instance.postgres.Stop(), os.RemoveAll(instance.runtimePath))
}

func createSharedTestPostgres() (*sharedTestPostgres, error) {
	runtimePath, err := os.MkdirTemp("", "toolhive-registry-postgres-*")
	if err != nil {
		return nil, fmt.Errorf("create shared Postgres runtime directory: %w", err)
	}

	postgres, cfg, err := startTestPostgres(runtimePath)
	if err != nil {
		_ = os.RemoveAll(runtimePath)
		return nil, err
	}

	return &sharedTestPostgres{
		postgres:    postgres,
		config:      cfg,
		runtimePath: runtimePath,
	}, nil
}

func releaseSharedTestDB(
	ctx context.Context,
	instance *sharedTestPostgres,
	databaseName string,
	db *pgx.Conn,
) error {
	closeErr := db.Close(ctx)

	sharedTestDB.Lock()
	defer sharedTestDB.Unlock()

	dropErr := dropSharedDatabase(ctx, instance, databaseName)

	instance.uses--
	if instance.uses > 0 {
		return errors.Join(closeErr, dropErr)
	}

	sharedTestDB.instance = nil
	return errors.Join(closeErr, dropErr, instance.postgres.Stop(), os.RemoveAll(instance.runtimePath))
}

func testConnectionURL(cfg embeddedpostgres.Config) string {
	return cfg.GetConnectionURL()
}
