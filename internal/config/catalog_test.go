package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCatalogRejectsRetiredPresenceFromSameDecode(t *testing.T) {
	t.Parallel()
	for _, field := range []string{
		"auth:\n  authz: null", "AUTH:\n  ROLES: {}", "auth.publicPaths: []",
		"sources:\n  - name: source\n    managed: {}\n    claims: null", "sources[0].claims: {}", "sources.0.claims.team: null",
		"registries:\n  - name: view\n    sources: [source]\n    Claims: {}",
		"old: &legacy {authz: null}\nauth:\n  <<: *legacy", "old: &legacy null\nauth:\n  publicPaths: *legacy",
	} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			file := filepath.Join(t.TempDir(), "config.yaml")
			data := "database: {host: localhost, port: 5432, user: test, database: test}\n" + field + "\n"
			require.NoError(t, os.WriteFile(file, []byte(data), 0600))
			var snapshot []byte
			cfg, err := LoadConfig(WithConfigPath(file), func(c *loaderConfig) error { c.exposureData = &snapshot; return nil })
			require.NoError(t, err)
			require.Equal(t, []byte(data), snapshot)
			require.ErrorContains(t, cfg.ValidateCatalogCutover(), "retired config fields")
			// Replacing the file cannot erase retained presence evidence.
			require.NoError(t, os.WriteFile(file, []byte("auth: {}"), 0600))
			require.ErrorContains(t, cfg.ValidateCatalogCutover(), "retired config fields")
		})
	}
}

//nolint:paralleltest // Environment snapshot tests cannot run in parallel.
func TestCatalogRejectsEmptyReservedEnvironmentPrefixes(t *testing.T) {
	for _, key := range []string{"AUTH_AUTHZ", "AUTH_AUTHZ_ROLES_SUPERADMIN", "AUTH_ROLES", "AUTH_PUBLICPATHS_0", "SOURCES_0_CLAIMS", "REGISTRIES_0_CLAIMS_TEAM", "SOURCES_0_CLAIMS_TOKEN", "AUTH.AUTHZ.ROLES"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(EnvPrefix+"_"+key, "")
			cfg := &Config{exposureEnv: exposureEnvironment()}
			require.ErrorContains(t, cfg.ValidateCatalogCutover(), "retired environment")
			require.NoError(t, os.Unsetenv(EnvPrefix+"_"+key))
			require.ErrorContains(t, cfg.ValidateCatalogCutover(), "retired environment", "validation must use frozen environment")
		})
	}
}
func TestCatalogDirectRetiredFieldsAndSecureGitSecret(t *testing.T) {
	t.Parallel()
	for _, cfg := range []*Config{{Auth: &AuthConfig{Authz: &AuthzConfig{}}}, {Auth: &AuthConfig{PublicPaths: []string{}}}, {Sources: []SourceConfig{{Claims: map[string]any{}}}}, {Registries: []RegistryConfig{{Claims: map[string]any{}}}}} {
		require.ErrorContains(t, cfg.ValidateCatalogCutover(), "retired")
	}
	file := filepath.Join(t.TempDir(), "secret")
	require.NoError(t, os.WriteFile(file, []byte(" synthetic-test-secret \n"), 0600))
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(file, link))
	value, err := LoadSecret(t.Context(), link)
	require.NoError(t, err)
	require.Equal(t, "synthetic-test-secret", value)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = LoadSecret(ctx, link)
	require.ErrorIs(t, err, context.Canceled)
	_, err = LoadSecret(t.Context(), "relative-missing")
	require.Error(t, err)
	require.False(t, strings.Contains(err.Error(), "relative-missing"))
}

//nolint:paralleltest // Effective environment snapshot must be stable across decoding and review.
func TestCatalogLoaderSupportsInlineAndFreezesEnvironment(t *testing.T) {
	t.Setenv(EnvPrefix+"_WATCH_NAMESPACE", "one,two")
	policy := filepath.Join(t.TempDir(), "deny.cedar")
	require.NoError(t, os.WriteFile(policy, []byte(`forbid(principal,action,resource);`), 0600))
	file := filepath.Join(t.TempDir(), "config.yaml")
	data := `database: {host: localhost, port: 5432, user: test, database: test}
auth: {mode: anonymous}
authorization:
  allowAnonymousCatalog: true
  policyFile: ` + policy + `
sources:
  - name: inline
    file: {data: '{"version":"1.0.0","data":{"servers":[]}}'}
registries:
  - name: view
    sources: [inline]
insecure_url: true
`
	require.NoError(t, os.WriteFile(file, []byte(data), 0600))
	cfg, err := LoadCatalogConfig(WithConfigPath(file))
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateCatalogCutover())
	require.NotEmpty(t, cfg.Sources[0].File.Data)
	require.Equal(t, "one,two", cfg.WatchNamespace)
	require.False(t, cfg.Auth.InsecureAllowHTTP, "YAML cannot set env-only security switches")
	_, review, err := LoadExposureConfig(file)
	require.NoError(t, err)
	revision, err := exposurePolicyRevision(policy)
	require.NoError(t, err)
	frozen, err := cfg.ExposureReview(revision)
	require.NoError(t, err)
	require.Equal(t, review.Revision, frozen.Revision)
	t.Setenv(EnvPrefix+"_WATCH_NAMESPACE", "three")
	frozenAfter, err := cfg.ExposureReview(revision)
	require.NoError(t, err)
	require.Equal(t, frozen, frozenAfter)
	_, err = LoadConfig(WithConfigPath(file))
	require.Error(t, err, "legacy CLI loader validation remains selected")
}
