package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestResolveAuthToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		cfg       *config.DatabaseConfig
		user      string
		wantToken string
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "nil config returns error",
			cfg:       nil,
			user:      "testuser",
			wantToken: "",
			wantErr:   true,
			errMsg:    "database configuration is required",
		},
		{
			name: "no dynamic auth returns empty token",
			cfg: &config.DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "appuser",
				Database: "testdb",
			},
			user:      "appuser",
			wantToken: "",
			wantErr:   false,
		},
		{
			name: "unknown dynamic auth type returns error",
			cfg: &config.DatabaseConfig{
				Host:        "localhost",
				Port:        5432,
				User:        "appuser",
				Database:    "testdb",
				DynamicAuth: &config.DynamicAuthConfig{
					// AWSRDSIAM is nil, so no known auth type is configured
				},
			},
			user:      "appuser",
			wantToken: "",
			wantErr:   true,
			errMsg:    "dynamic auth is configured but no supported auth method",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			token, err := ResolveAuthToken(ctx, tt.cfg, tt.user)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantToken, token)
		})
	}
}

func TestMigrationConnectionString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cfg         *config.DatabaseConfig
		wantConnStr string
		wantErr     bool
		errMsg      string
	}{
		{
			name:    "nil config returns error",
			cfg:     nil,
			wantErr: true,
			errMsg:  "database configuration is required",
		},
		{
			name: "no dynamic auth uses default user",
			cfg: &config.DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "appuser",
				Database: "testdb",
			},
			// No dynamic auth -> empty token -> connection string without password
			// GetMigrationUser() returns User when MigrationUser is not set
			wantConnStr: "postgres://appuser@localhost:5432/testdb?sslmode=require",
			wantErr:     false,
		},
		{
			name: "no dynamic auth with separate migration user",
			cfg: &config.DatabaseConfig{
				Host:          "db.example.com",
				Port:          5433,
				User:          "appuser",
				MigrationUser: "migratoruser",
				Database:      "production",
				SSLMode:       "verify-full",
			},
			// GetMigrationUser() returns MigrationUser when set
			wantConnStr: "postgres://migratoruser@db.example.com:5433/production?sslmode=verify-full",
			wantErr:     false,
		},
		{
			name: "no dynamic auth with default sslmode",
			cfg: &config.DatabaseConfig{
				Host:          "db.internal",
				Port:          5432,
				User:          "appuser",
				MigrationUser: "migrator",
				Database:      "mydb",
			},
			// SSLMode defaults to "require" when not set
			wantConnStr: "postgres://migrator@db.internal:5432/mydb?sslmode=require",
			wantErr:     false,
		},
		{
			name: "unknown dynamic auth type propagates error",
			cfg: &config.DatabaseConfig{
				Host:        "localhost",
				Port:        5432,
				User:        "appuser",
				Database:    "testdb",
				DynamicAuth: &config.DynamicAuthConfig{
					// AWSRDSIAM is nil -> unknown type
				},
			},
			wantErr: true,
			errMsg:  "failed to resolve auth token for migration user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			connStr, err := MigrationConnectionString(ctx, tt.cfg)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantConnStr, connStr)
		})
	}
}
