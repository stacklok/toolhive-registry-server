package writer

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
)

func TestNewSyncWriter(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, cleanupFunc := database.SetupTestDBContainer(t, ctx)
	t.Cleanup(cleanupFunc)

	// Migrate database for tests that need schema
	err := database.MigrateUp(ctx, db)
	require.NoError(t, err)

	tests := []struct {
		name    string
		pool    func() *pgxpool.Pool
		wantErr bool
		errMsg  string
	}{
		{
			name: "successfully creates database sync writer with pool",
			pool: func() *pgxpool.Pool {
				connStr := db.Config().ConnString()
				pool, err := pgxpool.New(ctx, connStr)
				require.NoError(t, err)
				t.Cleanup(func() { pool.Close() })
				return pool
			},
			wantErr: false,
		},
		{
			name: "returns error with nil pool",
			pool: func() *pgxpool.Pool {
				return nil
			},
			wantErr: true,
			errMsg:  "database pool is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pool := tt.pool()
			writer, err := NewSyncWriter(pool, 0)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, writer)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, writer)
		})
	}
}
