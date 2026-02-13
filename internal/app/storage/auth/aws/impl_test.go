package aws

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

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
			region, err := getRegion(ctx, tt.cfg)

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
