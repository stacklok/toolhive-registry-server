package writer

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/sources/mocks"
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
		name     string
		cfg      *config.Config
		setup    func(*gomock.Controller) (*mocks.MockStorageManager, error)
		wantType string // "file", "db", or "nil"
		wantErr  bool
		errMsg   string
	}{
		{
			name: "file storage with manager returns manager",
			cfg: &config.Config{
				FileStorage: &config.FileStorageConfig{
					BaseDir: "/tmp/test",
				},
			},
			setup: func(ctrl *gomock.Controller) (*mocks.MockStorageManager, error) {
				mockMgr := mocks.NewMockStorageManager(ctrl)
				return mockMgr, nil
			},
			wantType: "file",
			wantErr:  false,
		},
		{
			name: "database storage with pool returns db writer",
			cfg: &config.Config{
				Database: &config.DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					User:     "test",
					Database: "testdb",
				},
			},
			setup: func(_ *gomock.Controller) (*mocks.MockStorageManager, error) {
				// Return nil manager, pool will be passed separately in test
				return nil, nil
			},
			wantType: "db",
			wantErr:  false,
		},
		{
			name: "database storage with nil pool returns error",
			cfg: &config.Config{
				Database: &config.DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					User:     "test",
					Database: "testdb",
				},
			},
			setup: func(_ *gomock.Controller) (*mocks.MockStorageManager, error) {
				return nil, nil // nil manager and will pass nil pool
			},
			wantType: "nil",
			wantErr:  true,
			errMsg:   "database pool is required",
		},
		{
			name: "file storage with nil manager returns manager (nil)",
			cfg: &config.Config{
				FileStorage: &config.FileStorageConfig{
					BaseDir: "/tmp/test",
				},
			},
			setup: func(_ *gomock.Controller) (*mocks.MockStorageManager, error) {
				return nil, nil // nil manager allowed for file storage
			},
			wantType: "file",
			wantErr:  false,
		},
		{
			name: "no storage config defaults to file storage",
			cfg:  &config.Config{},
			setup: func(ctrl *gomock.Controller) (*mocks.MockStorageManager, error) {
				mockMgr := mocks.NewMockStorageManager(ctrl)
				return mockMgr, nil
			},
			wantType: "file",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			mockMgr, setupErr := tt.setup(ctrl)
			require.NoError(t, setupErr)

			var writer SyncWriter
			var err error

			switch tt.wantType {
			case "db":
				// For database tests, create a real pool
				connStr := db.Config().ConnString()
				pool, poolErr := pgxpool.New(ctx, connStr)
				require.NoError(t, poolErr)
				t.Cleanup(func() { pool.Close() })
				writer, err = NewSyncWriter(tt.cfg, mockMgr, pool)
			case "nil":
				// Pass nil pool to trigger error
				writer, err = NewSyncWriter(tt.cfg, mockMgr, nil)
			default:
				// For file tests, pass nil pool
				writer, err = NewSyncWriter(tt.cfg, mockMgr, nil)
			}

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, writer)
				return
			}

			require.NoError(t, err)

			// Verify we got the right type of writer
			switch tt.wantType {
			case "file":
				// For file storage, writer should be the storage manager (or nil if passed nil)
				if mockMgr != nil {
					assert.Equal(t, mockMgr, writer)
				}
			case "db":
				// For database storage, writer should be a database sync writer (not the mock manager)
				require.NotNil(t, writer)
				// Verify it's not the mock manager (it's a different type)
				assert.NotEqual(t, mockMgr, writer)
			}
		})
	}
}

func TestNewSyncWriter_DatabaseMode_Integration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, cleanupFunc := database.SetupTestDBContainer(t, ctx)
	t.Cleanup(cleanupFunc)

	// Migrate database
	err := database.MigrateUp(ctx, db)
	require.NoError(t, err)

	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr bool
	}{
		{
			name: "successfully creates database sync writer with real pool",
			cfg: &config.Config{
				Database: &config.DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					User:     "test",
					Database: "testdb",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create real database pool
			connStr := db.Config().ConnString()
			pool, err := pgxpool.New(ctx, connStr)
			require.NoError(t, err)
			t.Cleanup(func() { pool.Close() })

			writer, err := NewSyncWriter(tt.cfg, nil, pool)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, writer)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, writer)

			// Verify the writer implements SyncWriter and is not nil
			// We can't check the concrete type since dbSyncWriter is unexported
			assert.NotNil(t, writer)
		})
	}
}

func TestNewSyncWriter_FileMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr bool
	}{
		{
			name: "file mode with mock storage manager",
			cfg: &config.Config{
				FileStorage: &config.FileStorageConfig{
					BaseDir: "/tmp/test",
				},
			},
			wantErr: false,
		},
		{
			name: "file mode with nil storage manager (allowed)",
			cfg: &config.Config{
				FileStorage: &config.FileStorageConfig{
					BaseDir: "/tmp/test",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			var mockMgr *mocks.MockStorageManager
			if tt.name == "file mode with mock storage manager" {
				mockMgr = mocks.NewMockStorageManager(ctrl)
			}

			writer, err := NewSyncWriter(tt.cfg, mockMgr, nil)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, writer)
				return
			}

			require.NoError(t, err)

			// For file mode, the writer IS the storage manager
			if mockMgr != nil {
				assert.Equal(t, mockMgr, writer)
			}
		})
	}
}
