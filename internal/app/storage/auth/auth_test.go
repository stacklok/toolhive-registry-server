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

func TestResolveAWSRegion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		cfg        *config.DatabaseConfig
		wantRegion string
		wantErr    bool
		errMsg     string
	}{
		{
			name: "static region configured returns region string",
			cfg: &config.DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "appuser",
				Database: "testdb",
				DynamicAuth: &config.DynamicAuthConfig{
					AWSRDSIAM: &config.DynamicAuthAWSRDSIAM{
						Region: "us-east-1",
					},
				},
			},
			wantRegion: "us-east-1",
			wantErr:    false,
		},
		{
			name: "empty region returns error",
			cfg: &config.DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "appuser",
				Database: "testdb",
				DynamicAuth: &config.DynamicAuthConfig{
					AWSRDSIAM: &config.DynamicAuthAWSRDSIAM{
						Region: "",
					},
				},
			},
			wantRegion: "",
			wantErr:    true,
			errMsg:     "AWS RDS IAM region is not configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			region, err := resolveAWSRegion(ctx, tt.cfg)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantRegion, region)
		})
	}
}

func TestNewDynamicAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     *config.DatabaseConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil config returns error",
			cfg:     nil,
			wantErr: true,
			errMsg:  "database configuration is required",
		},
		{
			name: "nil DynamicAuth returns error",
			cfg: &config.DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "appuser",
				Database: "testdb",
			},
			wantErr: true,
			errMsg:  "dynamic authentication is not configured",
		},
		{
			name: "unknown auth type returns error",
			cfg: &config.DatabaseConfig{
				Host:        "localhost",
				Port:        5432,
				User:        "appuser",
				Database:    "testdb",
				DynamicAuth: &config.DynamicAuthConfig{
					// AWSRDSIAM is nil, so no known auth type is configured
				},
			},
			wantErr: true,
			errMsg:  "dynamic auth is configured but no supported auth method",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			authFunc, err := NewDynamicAuth(ctx, tt.cfg)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, authFunc)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, authFunc)
		})
	}
}

func TestResolveAuthTokenWithAWSEmptyRegion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := &config.DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "appuser",
		Database: "testdb",
		DynamicAuth: &config.DynamicAuthConfig{
			AWSRDSIAM: &config.DynamicAuthAWSRDSIAM{
				Region: "",
			},
		},
	}

	token, err := ResolveAuthToken(ctx, cfg, "appuser")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "AWS RDS IAM region is not configured")
	assert.Empty(t, token)
}

func TestMigrationConnectionStringWithAWSEmptyRegion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := &config.DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "appuser",
		Database: "testdb",
		DynamicAuth: &config.DynamicAuthConfig{
			AWSRDSIAM: &config.DynamicAuthAWSRDSIAM{
				Region: "",
			},
		},
	}

	connStr, err := MigrationConnectionString(ctx, cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to resolve auth token for migration user")
	assert.Empty(t, connStr)
}
