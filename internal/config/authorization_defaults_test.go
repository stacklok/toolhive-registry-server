package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/authorization"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestOfflineAndServingEffectivePolicyRevision(t *testing.T) {
	t.Parallel()
	for _, block := range []string{"", "authorization: {}\n", "authorization: {policyFile: ''}\n"} {
		t.Run(block, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(`database:
  host: localhost
  port: 5432
  user: registry
  database: registry
  sslMode: disable
auth:
  mode: oauth
  oauth:
    providers:
      - name: idp
        issuerUrl: https://id.example.org
        audience: registry
`+block), 0600))
			_, offline, err := config.LoadExposureConfig(path)
			require.NoError(t, err)
			cfg, err := config.LoadCatalogConfig(config.WithConfigPath(path))
			require.NoError(t, err)
			require.NoError(t, cfg.ValidateAdmission())
			engine, err := authorization.Load(cfg.Authorization.PolicyFile)
			require.NoError(t, err)
			serving, err := cfg.ExposureReview(engine.Revision())
			require.NoError(t, err)
			require.Equal(t, offline, serving, "all offline phases and finalize/startup must bind the evaluated default, not absent")
		})
	}
}

//nolint:paralleltest // Environment overrides are process-global.
func TestPolicyEnvironmentOverrideWithoutYAMLKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("database: {host: localhost, port: 5432, user: registry, database: registry, sslMode: disable}\n"), 0600))
	t.Setenv("THV_REGISTRY_AUTHORIZATION_POLICYFILE", "/nonexistent/explicit-policy.cedar")
	cfg, err := config.LoadCatalogConfig(config.WithConfigPath(path))
	require.NoError(t, err)
	require.Equal(t, "/nonexistent/explicit-policy.cedar", cfg.Authorization.PolicyFile)
	_, _, err = config.LoadExposureConfig(path)
	require.Error(t, err, "explicit environment file must not fall back to default")
	_, err = authorization.Load(cfg.Authorization.PolicyFile)
	require.Error(t, err)
}
