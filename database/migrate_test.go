package database

import (
	"context"
	"io/fs"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, cleanupFunc := SetupTestDBContainer(t, ctx)
	t.Cleanup(cleanupFunc)

	connString := db.Config().ConnString()

	// Create migrate instance
	m, err := GetMigrate(connString)
	assert.NoError(t, err)
	defer m.Close()

	// Count the number of logical migrations
	fnames, err := fs.Glob(migrationsFS, "migrations/*.up.sql")
	require.NoError(t, err)

	// Test each migration individually: apply, rollback, re-apply
	for i := 1; i <= len(fnames); i++ {
		// Apply one migration
		err = m.Steps(1)
		assert.NoError(t, err, "Failed to apply migration %d", i)

		// Roll back one migration
		err = m.Steps(-1)
		assert.NoError(t, err, "Failed to roll back migration %d", i)

		// Re-apply the same migration
		err = m.Steps(1)
		assert.NoError(t, err, "Failed to re-apply migration %d", i)
	}

	// Test rolling back all migrations
	err = m.Down()
	assert.NoError(t, err, "Failed to roll back all migrations")

	// Test applying all migrations at once
	err = m.Up()
	assert.NoError(t, err, "Failed to apply all migrations")
}

func TestGetMigrate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, cleanupFunc := SetupTestDBContainer(t, ctx)
	t.Cleanup(cleanupFunc)

	tests := []struct {
		name       string
		connString string
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "valid connection string",
			connString: db.Config().ConnString(),
			wantErr:    false,
		},
		{
			name:       "invalid connection string format",
			connString: "invalid://connection",
			wantErr:    true,
			errMsg:     "failed to create migrate instance",
		},
		{
			name:       "empty connection string",
			connString: "",
			wantErr:    true,
			errMsg:     "failed to create migrate instance",
		},
		{
			name:       "malformed postgres connection",
			connString: "postgres://",
			wantErr:    true,
			errMsg:     "failed to create migrate instance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m, err := GetMigrate(tt.connString)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, m)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, m)
			defer m.Close()
		})
	}
}

func TestMigrateUp(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func(*testing.T) *pgx.Conn
		wantErr bool
		errMsg  string
	}{
		{
			name: "fresh database - apply all migrations",
			setup: func(t *testing.T) *pgx.Conn {
				t.Helper()
				db, cleanup := SetupTestDBContainer(t, ctx)
				t.Cleanup(cleanup)
				return db
			},
			wantErr: false,
		},
		{
			name: "already migrated database - no change",
			setup: func(t *testing.T) *pgx.Conn {
				t.Helper()
				db, cleanup := SetupTestDBContainer(t, ctx)
				t.Cleanup(cleanup)
				// Run migrations once
				err := MigrateUp(ctx, db)
				require.NoError(t, err)
				return db
			},
			wantErr: false,
		},
		{
			name: "partially migrated database - apply remaining",
			setup: func(t *testing.T) *pgx.Conn {
				t.Helper()
				db, cleanup := SetupTestDBContainer(t, ctx)
				t.Cleanup(cleanup)
				// Apply first migration only
				m, err := GetMigrate(db.Config().ConnString())
				require.NoError(t, err)
				err = m.Steps(1)
				require.NoError(t, err)
				m.Close()
				return db
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := tt.setup(t)
			err := MigrateUp(ctx, db)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestMigrateDown(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func(*testing.T) (*pgx.Conn, int)
		steps   int
		wantErr bool
		errMsg  string
	}{
		{
			name: "rollback one step from fully migrated",
			setup: func(t *testing.T) (*pgx.Conn, int) {
				t.Helper()
				db, cleanup := SetupTestDBContainer(t, ctx)
				t.Cleanup(cleanup)
				err := MigrateUp(ctx, db)
				require.NoError(t, err)
				return db, 1
			},
			steps:   1,
			wantErr: false,
		},
		{
			name: "rollback multiple steps",
			setup: func(t *testing.T) (*pgx.Conn, int) {
				t.Helper()
				db, cleanup := SetupTestDBContainer(t, ctx)
				t.Cleanup(cleanup)
				err := MigrateUp(ctx, db)
				require.NoError(t, err)
				// Count available migrations
				fnames, err := fs.Glob(migrationsFS, "migrations/*.up.sql")
				require.NoError(t, err)
				return db, len(fnames)
			},
			steps:   2,
			wantErr: false,
		},
		{
			name: "rollback zero steps - no-op",
			setup: func(t *testing.T) (*pgx.Conn, int) {
				t.Helper()
				db, cleanup := SetupTestDBContainer(t, ctx)
				t.Cleanup(cleanup)
				err := MigrateUp(ctx, db)
				require.NoError(t, err)
				return db, 0
			},
			steps:   0,
			wantErr: false,
		},
		{
			name: "rollback on empty database - returns error",
			setup: func(t *testing.T) (*pgx.Conn, int) {
				t.Helper()
				db, cleanup := SetupTestDBContainer(t, ctx)
				t.Cleanup(cleanup)
				return db, 1
			},
			steps:   1,
			wantErr: true,
			errMsg:  "failed to revert migrations",
		},
		{
			name: "rollback all migrations",
			setup: func(t *testing.T) (*pgx.Conn, int) {
				t.Helper()
				db, cleanup := SetupTestDBContainer(t, ctx)
				t.Cleanup(cleanup)
				err := MigrateUp(ctx, db)
				require.NoError(t, err)
				// Count available migrations
				fnames, err := fs.Glob(migrationsFS, "migrations/*.up.sql")
				require.NoError(t, err)
				return db, len(fnames)
			},
			steps:   0, // Will be set from setup
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, setupSteps := tt.setup(t)
			steps := tt.steps
			if tt.name == "rollback all migrations" {
				steps = setupSteps
			}

			err := MigrateDown(ctx, db, steps)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestGetPrimeTemplate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		wantContains []string
		wantErr      bool
	}{
		{
			name: "template loads successfully",
			wantContains: []string{
				"CREATE ROLE",
				"CREATE USER",
				"GRANT",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			content, err := GetPrimeTemplate()

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, content)
			assert.NotEmpty(t, content)

			// Verify content contains expected SQL keywords
			contentStr := string(content)
			for _, substr := range tt.wantContains {
				assert.Contains(t, contentStr, substr,
					"template should contain '%s'", substr)
			}
		})
	}
}
