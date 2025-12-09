package database

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	tclog "github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type nopLogger struct{}

func (*nopLogger) Printf(_ string, _ ...any) {}

var _ tclog.Logger = (*nopLogger)(nil)

const (
	// DBName is the name of the test database
	DBName = "testdb"
	// DBUser is the username for the root user of the test database
	DBUser = "testuser"
	// DBPass is the password for the root user of the test database
	DBPass = "testpass"
)

// SetupTestDBContainer creates a Postgres container using testcontainers and returns a connection to the database
//
//nolint:revive
func SetupTestDBContainer(t *testing.T, ctx context.Context) (*pgx.Conn, func()) {
	t.Helper()

	// Start Postgres container
	postgresContainer, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase(DBName),
		postgres.WithUsername(DBUser),
		postgres.WithPassword(DBPass),
		postgres.BasicWaitStrategies(),
		tc.WithLogger(&nopLogger{}),
		tc.WithCmd("postgres", "-c", "fsync=off", "-c", "log_statement=all"),
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
	db, containerCleanupFunc := SetupTestDBContainer(t, ctx)

	// Apply migrations
	err := MigrateUp(ctx, db)
	require.NoError(t, err)

	// Register custom array codecs for enum types
	err = registerTestArrayCodecs(ctx, db)
	require.NoError(t, err)

	cleanupFunc := func() {
		//nolint:gosec
		_ = db.Close(ctx)
		containerCleanupFunc()
	}

	return db, cleanupFunc
}

// registerTestArrayCodecs registers codecs for all custom enum array types
// This is needed for tests that use array parameters with custom enum types
func registerTestArrayCodecs(ctx context.Context, conn *pgx.Conn) error {
	enumTypes := []string{"registry_type", "sync_status", "icon_theme", "creation_type"}

	for _, enumName := range enumTypes {
		var enumOID uint32
		err := conn.QueryRow(ctx, "SELECT oid FROM pg_type WHERE typname = $1", enumName).Scan(&enumOID)
		if err != nil {
			return fmt.Errorf("failed to get %s OID: %w", enumName, err)
		}

		var arrayOID uint32
		err = conn.QueryRow(ctx, "SELECT oid FROM pg_type WHERE typname = $1", "_"+enumName).Scan(&arrayOID)
		if err != nil {
			return fmt.Errorf("failed to get %s[] array OID: %w", enumName, err)
		}

		conn.TypeMap().RegisterType(&pgtype.Type{
			Name: enumName + "[]",
			OID:  arrayOID,
			Codec: &pgtype.ArrayCodec{
				ElementType: &pgtype.Type{
					Name:  enumName,
					OID:   enumOID,
					Codec: pgtype.TextCodec{},
				},
			},
		})
	}

	return nil
}
