package app

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
)

func TestInitializeManagedRegistries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		config        *config.Config
		expectedError string
		expectedCount int
	}{
		{
			name:          "nil config",
			config:        nil,
			expectedError: "config is required",
			expectedCount: 0,
		},
		{
			name: "no managed registries",
			config: &config.Config{
				Registries: []config.RegistryConfig{
					{
						Name: "git-registry",
						Git: &config.GitConfig{
							Repository: "https://github.com/example/repo",
						},
					},
					{
						Name: "api-registry",
						API: &config.APIConfig{
							Endpoint: "https://api.example.com",
						},
					},
				},
			},
			expectedError: "",
			expectedCount: 0,
		},
		{
			name: "single managed registry",
			config: &config.Config{
				Registries: []config.RegistryConfig{
					{
						Name:    "managed-registry",
						Managed: &config.ManagedConfig{},
					},
				},
			},
			expectedError: "",
			expectedCount: 1,
		},
		{
			name: "multiple managed registries",
			config: &config.Config{
				Registries: []config.RegistryConfig{
					{
						Name:    "managed-registry-1",
						Managed: &config.ManagedConfig{},
					},
					{
						Name: "git-registry",
						Git: &config.GitConfig{
							Repository: "https://github.com/example/repo",
						},
					},
					{
						Name:    "managed-registry-2",
						Managed: &config.ManagedConfig{},
					},
				},
			},
			expectedError: "",
			expectedCount: 2,
		},
		{
			name: "idempotent - can run multiple times",
			config: &config.Config{
				Registries: []config.RegistryConfig{
					{
						Name:    "idempotent-registry",
						Managed: &config.ManagedConfig{},
					},
				},
			},
			expectedError: "",
			expectedCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()

			// Setup test database
			var pool *pgxpool.Pool
			var cleanup func()

			if tt.config != nil {
				db, cleanupFunc := database.SetupTestDB(t)
				defer cleanupFunc()

				connStr := db.Config().ConnString()
				var err error
				pool, err = pgxpool.New(ctx, connStr)
				require.NoError(t, err)
				defer pool.Close()

				cleanup = func() {
					pool.Close()
					cleanupFunc()
				}
				defer cleanup()
			}

			// Run initialization
			err := InitializeManagedRegistries(ctx, tt.config, pool)

			// Assert expectations
			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				assert.NoError(t, err)

				// Verify registries were created if pool exists
				if pool != nil && tt.expectedCount > 0 {
					queries := sqlc.New(pool)
					registries, err := queries.ListRegistries(ctx, sqlc.ListRegistriesParams{
						Size: 100,
					})
					require.NoError(t, err)
					assert.Len(t, registries, tt.expectedCount)

					// Verify all managed registries have LOCAL type
					for _, reg := range registries {
						assert.Equal(t, sqlc.RegistryTypeLOCAL, reg.RegType)
					}
				}
			}

			// Test idempotency - run again and verify it doesn't error
			if tt.name == "idempotent - can run multiple times" && pool != nil {
				// Run initialization again
				err := InitializeManagedRegistries(ctx, tt.config, pool)
				assert.NoError(t, err)

				// Verify still only one registry
				queries := sqlc.New(pool)
				registries, err := queries.ListRegistries(ctx, sqlc.ListRegistriesParams{
					Size: 100,
				})
				require.NoError(t, err)
				assert.Len(t, registries, 1)
			}
		})
	}
}

func TestInitializeManagedRegistries_NilPool(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Registries: []config.RegistryConfig{
			{
				Name:    "managed-registry",
				Managed: &config.ManagedConfig{},
			},
		},
	}

	err := InitializeManagedRegistries(context.Background(), cfg, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database pool is required")
}

func TestPluralize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		count    int
		singular string
		plural   string
		expected string
	}{
		{
			name:     "single item",
			count:    1,
			singular: "y",
			plural:   "ies",
			expected: "y",
		},
		{
			name:     "zero items",
			count:    0,
			singular: "y",
			plural:   "ies",
			expected: "ies",
		},
		{
			name:     "multiple items",
			count:    5,
			singular: "y",
			plural:   "ies",
			expected: "ies",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := pluralize(tt.count, tt.singular, tt.plural)
			assert.Equal(t, tt.expected, result)
		})
	}
}
