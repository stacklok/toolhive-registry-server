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

// SetupTestDB creates a Postgres container using testcontainers and runs migrations
func SetupTestDB(t *testing.T) (*pgx.Conn, func()) {
	t.Helper()

	ctx := context.Background()

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

	// Run migrations
	err = MigrateUp(ctx, db)
	require.NoError(t, err)

	// Test full migration rollback (migrate down by all steps)
	err = MigrateDown(ctx, db, 1)
	require.NoError(t, err)

	// Reapply migrations
	err = MigrateUp(ctx, db)
	require.NoError(t, err)

	cleanupFunc := func() {
		//nolint:gosec
		_ = db.Close(ctx)
		tc.CleanupContainer(t, postgresContainer)
	}

	return db, cleanupFunc
}
