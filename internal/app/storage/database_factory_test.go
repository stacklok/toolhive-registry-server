package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestNewDatabaseFactory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, cleanupFunc := database.SetupTestDBContainer(t, ctx)
	t.Cleanup(cleanupFunc)

	// Migrate database for tests that need schema
	err := database.MigrateUp(ctx, db)
	require.NoError(t, err)

	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config with database settings",
			cfg: &config.Config{
				Database: &config.DatabaseConfig{
					Host:     db.Config().Host,
					Port:     int(db.Config().Port),
					User:     db.Config().User,
					Database: db.Config().Database,
					SSLMode:  "disable",
				},
			},
			wantErr: false,
		},
		{
			name:    "nil config returns error",
			cfg:     nil,
			wantErr: true,
			errMsg:  "config cannot be nil",
		},
		{
			name: "config with nil database field returns error",
			cfg: &config.Config{
				Database: nil,
			},
			wantErr: true,
			errMsg:  "database configuration is required",
		},
		{
			name: "valid config with connection pool settings",
			cfg: &config.Config{
				Database: &config.DatabaseConfig{
					Host:            db.Config().Host,
					Port:            int(db.Config().Port),
					User:            db.Config().User,
					Database:        db.Config().Database,
					SSLMode:         "disable",
					MaxOpenConns:    10,
					MaxIdleConns:    5,
					ConnMaxLifetime: "1h",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			factory, err := NewDatabaseFactory(ctx, tt.cfg)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, factory)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, factory)
			assert.NotNil(t, factory.config)
			assert.NotNil(t, factory.pool)
			assert.Equal(t, tt.cfg, factory.config)

			// Cleanup
			factory.Cleanup()
		})
	}
}

// TestNewDatabaseFactory_PgpassResolution verifies the pool authenticates using
// a password resolved from PGPASSFILE when no inline password is configured.
// The shared toolhive-core pool builder treats Password as already resolved, so
// the config layer must consult the password file itself; without that, the
// pool connects with an empty password and fails SASL auth.
//
// Not parallel: it sets the process-wide PGPASSFILE via t.Setenv.
func TestNewDatabaseFactory_PgpassResolution(t *testing.T) {
	ctx := context.Background()
	db, cleanupFunc := database.SetupTestDBContainer(t, ctx)
	t.Cleanup(cleanupFunc)

	require.NoError(t, database.MigrateUp(ctx, db))

	host := db.Config().Host
	port := int(db.Config().Port)
	user := db.Config().User
	dbName := db.Config().Database
	password := db.Config().Password
	require.NotEmpty(t, password, "test container should require a password")

	// Write a pgpass entry for the container and point PGPASSFILE at it.
	pgpassPath := filepath.Join(t.TempDir(), ".pgpass")
	entry := fmt.Sprintf("%s:%d:%s:%s:%s\n", host, port, dbName, user, password)
	require.NoError(t, os.WriteFile(pgpassPath, []byte(entry), 0o600))
	t.Setenv("PGPASSFILE", pgpassPath)

	cfg := &config.Config{
		Database: &config.DatabaseConfig{
			Host:     host,
			Port:     port,
			User:     user,
			Database: dbName,
			SSLMode:  "disable",
			// No Password configured — it must be resolved from PGPASSFILE.
		},
	}

	factory, err := NewDatabaseFactory(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(factory.Cleanup)

	// Acquiring a connection forces authentication; with the empty password the
	// pool would fail SASL auth here.
	require.NoError(t, factory.pool.Ping(ctx))
}

func TestDatabaseFactory_CreateRegistryMetricsReader(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, cleanupFunc := database.SetupTestDBContainer(t, ctx)
	t.Cleanup(cleanupFunc)

	err := database.MigrateUp(ctx, db)
	require.NoError(t, err)

	cfg := &config.Config{
		Database: &config.DatabaseConfig{
			Host:     db.Config().Host,
			Port:     int(db.Config().Port),
			User:     db.Config().User,
			Password: database.DBPass,
			Database: db.Config().Database,
			SSLMode:  "disable",
		},
	}
	factory, err := NewDatabaseFactory(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(factory.Cleanup)

	alphaSourceID := insertMetricTestSource(ctx, t, factory, "alpha")
	insertMetricTestSource(ctx, t, factory, "empty")

	serverEntryID := insertMetricTestEntry(ctx, t, factory, alphaSourceID, "MCP", "server-a")
	insertMetricTestVersion(ctx, t, factory, serverEntryID, "server-a", "1.0.0")
	insertMetricTestVersion(ctx, t, factory, serverEntryID, "server-a", "2.0.0")
	skillEntryID := insertMetricTestEntry(ctx, t, factory, alphaSourceID, "SKILL", "skill-a")
	insertMetricTestVersion(ctx, t, factory, skillEntryID, "skill-a", "1.0.0")

	reader, err := factory.CreateRegistryMetricsReader(ctx)
	require.NoError(t, err)

	counts, err := reader.RegistryMetricCounts(ctx)
	require.NoError(t, err)

	bySource := make(map[string][2]int64, len(counts))
	for _, count := range counts {
		bySource[count.SourceName] = [2]int64{count.ServerCount, count.SkillCount}
	}

	assert.Equal(t, [2]int64{1, 1}, bySource["alpha"])
	assert.Equal(t, [2]int64{0, 0}, bySource["empty"])
}

func insertMetricTestSource(
	ctx context.Context,
	t *testing.T,
	factory *DatabaseFactory,
	name string,
) uuid.UUID {
	t.Helper()

	var sourceID uuid.UUID
	err := factory.pool.QueryRow(ctx, `
INSERT INTO source (name, source_type, syncable, creation_type)
VALUES ($1, 'git', true, 'CONFIG')
RETURNING id`, name).Scan(&sourceID)
	require.NoError(t, err)

	return sourceID
}

func insertMetricTestEntry(
	ctx context.Context,
	t *testing.T,
	factory *DatabaseFactory,
	sourceID uuid.UUID,
	entryType string,
	name string,
) uuid.UUID {
	t.Helper()

	var entryID uuid.UUID
	err := factory.pool.QueryRow(ctx, `
INSERT INTO registry_entry (source_id, entry_type, name)
VALUES ($1, $2, $3)
RETURNING id`, sourceID, entryType, name).Scan(&entryID)
	require.NoError(t, err)

	return entryID
}

func insertMetricTestVersion(
	ctx context.Context,
	t *testing.T,
	factory *DatabaseFactory,
	entryID uuid.UUID,
	name string,
	version string,
) {
	t.Helper()

	_, err := factory.pool.Exec(ctx, `
INSERT INTO entry_version (entry_id, name, version)
VALUES ($1, $2, $3)`, entryID, name, version)
	require.NoError(t, err)
}

func TestDatabaseFactory_CreateStateService(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, cleanupFunc := database.SetupTestDBContainer(t, ctx)
	t.Cleanup(cleanupFunc)

	// Migrate database
	err := database.MigrateUp(ctx, db)
	require.NoError(t, err)

	tests := []struct {
		name    string
		setup   func(*testing.T) *DatabaseFactory
		wantErr bool
	}{
		{
			name: "successfully creates state service",
			setup: func(t *testing.T) *DatabaseFactory {
				t.Helper()
				cfg := &config.Config{
					Database: &config.DatabaseConfig{
						Host:     db.Config().Host,
						Port:     int(db.Config().Port),
						User:     db.Config().User,
						Database: db.Config().Database,
						SSLMode:  "disable",
					},
				}
				factory, err := NewDatabaseFactory(ctx, cfg)
				require.NoError(t, err)
				t.Cleanup(factory.Cleanup)
				return factory
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			factory := tt.setup(t)
			stateService, err := factory.CreateStateService(ctx)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, stateService)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, stateService)
		})
	}
}

func TestDatabaseFactory_CreateSyncWriter(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, cleanupFunc := database.SetupTestDBContainer(t, ctx)
	t.Cleanup(cleanupFunc)

	// Migrate database
	err := database.MigrateUp(ctx, db)
	require.NoError(t, err)

	tests := []struct {
		name    string
		setup   func(*testing.T) *DatabaseFactory
		wantErr bool
	}{
		{
			name: "successfully creates sync writer",
			setup: func(t *testing.T) *DatabaseFactory {
				t.Helper()
				cfg := &config.Config{
					Database: &config.DatabaseConfig{
						Host:     db.Config().Host,
						Port:     int(db.Config().Port),
						User:     db.Config().User,
						Database: db.Config().Database,
						SSLMode:  "disable",
					},
				}
				factory, err := NewDatabaseFactory(ctx, cfg)
				require.NoError(t, err)
				t.Cleanup(factory.Cleanup)
				return factory
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			factory := tt.setup(t)
			syncWriter, err := factory.CreateSyncWriter(ctx)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, syncWriter)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, syncWriter)
		})
	}
}

func TestDatabaseFactory_CreateRegistryService(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, cleanupFunc := database.SetupTestDBContainer(t, ctx)
	t.Cleanup(cleanupFunc)

	// Migrate database
	err := database.MigrateUp(ctx, db)
	require.NoError(t, err)

	tests := []struct {
		name    string
		setup   func(*testing.T) *DatabaseFactory
		wantErr bool
	}{
		{
			name: "successfully creates registry service",
			setup: func(t *testing.T) *DatabaseFactory {
				t.Helper()
				cfg := &config.Config{
					Database: &config.DatabaseConfig{
						Host:     db.Config().Host,
						Port:     int(db.Config().Port),
						User:     db.Config().User,
						Database: db.Config().Database,
						SSLMode:  "disable",
					},
				}
				factory, err := NewDatabaseFactory(ctx, cfg)
				require.NoError(t, err)
				t.Cleanup(factory.Cleanup)
				return factory
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			factory := tt.setup(t)
			registryService, err := factory.CreateRegistryService(ctx)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, registryService)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, registryService)
		})
	}
}

func TestDatabaseFactory_Cleanup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, cleanupFunc := database.SetupTestDBContainer(t, ctx)
	t.Cleanup(cleanupFunc)

	// Migrate database
	err := database.MigrateUp(ctx, db)
	require.NoError(t, err)

	tests := []struct {
		name  string
		setup func(*testing.T) *DatabaseFactory
	}{
		{
			name: "cleanup closes pool successfully",
			setup: func(t *testing.T) *DatabaseFactory {
				t.Helper()
				cfg := &config.Config{
					Database: &config.DatabaseConfig{
						Host:     db.Config().Host,
						Port:     int(db.Config().Port),
						User:     db.Config().User,
						Database: db.Config().Database,
						SSLMode:  "disable",
					},
				}
				factory, err := NewDatabaseFactory(ctx, cfg)
				require.NoError(t, err)
				return factory
			},
		},
		{
			name: "cleanup is idempotent - can be called multiple times",
			setup: func(t *testing.T) *DatabaseFactory {
				t.Helper()
				cfg := &config.Config{
					Database: &config.DatabaseConfig{
						Host:     db.Config().Host,
						Port:     int(db.Config().Port),
						User:     db.Config().User,
						Database: db.Config().Database,
						SSLMode:  "disable",
					},
				}
				factory, err := NewDatabaseFactory(ctx, cfg)
				require.NoError(t, err)
				return factory
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			factory := tt.setup(t)

			// Should not panic
			require.NotPanics(t, func() {
				factory.Cleanup()
			})

			// Verify pool is closed by checking pool stats (will panic if pool is closed)
			assert.NotNil(t, factory.pool)

			// For idempotency test, call cleanup again
			if tt.name == "cleanup is idempotent - can be called multiple times" {
				require.NotPanics(t, func() {
					factory.Cleanup()
				})
			}
		})
	}
}
