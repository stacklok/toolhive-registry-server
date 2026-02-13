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
			token, err := NewAuthToken(ctx, tt.cfg, tt.user)

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

func TestNewDynamicAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     *config.DatabaseConfig
		user    string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil config returns error",
			cfg:     nil,
			user:    "testuser",
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
			user:    "appuser",
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
			user:    "appuser",
			wantErr: true,
			errMsg:  "dynamic auth is configured but no supported auth method",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			authFunc, err := NewDynamicAuth(ctx, tt.cfg, tt.user)

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

	token, err := NewAuthToken(ctx, cfg, "appuser")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "AWS RDS IAM region is not configured")
	assert.Empty(t, token)
}
