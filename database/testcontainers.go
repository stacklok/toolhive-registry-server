package database

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	tclog "github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type nopLogger struct{}

func (*nopLogger) Printf(_ string, _ ...any) {}

var _ tclog.Logger = (*nopLogger)(nil)

var (
	dbName = "testdb"
	dbUser = "testuser"
	dbPass = "testpass"
)

// SetupTestDBContaienr creates a Postgres container using testcontainers and returns a connection to the database
//
//nolint:revive
func SetupTestDBContaienr(t *testing.T, ctx context.Context) (*pgx.Conn, func()) {
	t.Helper()

	// Start Postgres container
	postgresContainer, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase(dbName),
		postgres.WithUsername(dbUser),
		postgres.WithPassword(dbPass),
		postgres.BasicWaitStrategies(),
		tc.WithLogger(&nopLogger{}),
	)
	require.NoError(t, err)

	// Get connection string
	connStr, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	// Connect to database with retry logic
	var db *pgx.Conn
	db, err = pgx.Connect(ctx, connStr)
	require.NoError(t, err)

	return db, func() { tc.CleanupContainer(t, postgresContainer) }
}

// SetupTestDB creates a Postgres container using testcontainers and runs all migrations
func SetupTestDB(t *testing.T) (*pgx.Conn, func()) {
	t.Helper()

	ctx := context.Background()
	db, containerCleanupFunc := SetupTestDBContaienr(t, ctx)

	// Apply migrations
	err := MigrateUp(ctx, db)
	require.NoError(t, err)

	cleanupFunc := func() {
		//nolint:gosec
		_ = db.Close(ctx)
		containerCleanupFunc()
	}

	return db, cleanupFunc
}
