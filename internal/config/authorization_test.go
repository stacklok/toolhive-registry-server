package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const authzTestBase = `
sources:
  - name: local
    file: {path: /data/registry.json}
    syncPolicy: {interval: 30m}
registries:
  - name: main
    sources: [local]
database: {host: localhost, port: 5432, user: u, database: d}
`

const oauthBlock = `
auth:
  mode: oauth
  oauth:
    resourceUrl: https://registry.example.com
    providers:
      - name: idp
        issuerUrl: https://idp.example.com
        audience: registry
`

func loadAuthzConfig(t *testing.T, yaml string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(authzTestBase+yaml), 0600))
	return LoadConfig(WithConfigPath(path))
}

func TestAuthorizationModeSelection(t *testing.T) {
	t.Parallel()
	cfg, err := loadAuthzConfig(t, "auth: {mode: anonymous}\n")
	require.NoError(t, err)
	assert.Equal(t, AuthorizationModeClaims, cfg.AuthorizationMode(), "no authorization block is claims mode")
	assert.True(t, cfg.AdminEnabled(), "claims mode always serves /v1")

	cfg, err = loadAuthzConfig(t, oauthBlock+"authorization: {}\n")
	require.NoError(t, err)
	assert.Equal(t, AuthorizationModeCedar, cfg.AuthorizationMode(), "an empty authorization block selects Cedar")
	assert.False(t, cfg.AdminEnabled(), "Cedar mode serves /v1 only when enabled")

	cfg, err = loadAuthzConfig(t, oauthBlock+"authorization: {policyFile: /etc/registry/policy.cedar}\nadmin: {enabled: true}\n")
	require.NoError(t, err)
	assert.True(t, cfg.AdminEnabled())
	assert.Equal(t, "/etc/registry/policy.cedar", cfg.Authorization.PolicyFile)
}

func TestAuthorizationModeValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, yaml, wantErr string
	}{
		{"admin block in claims mode", "auth: {mode: anonymous}\nadmin: {enabled: true}\n", "admin applies to Cedar mode only"},
		{"roles in Cedar mode", oauthBlock + "  authz:\n    roles:\n      superAdmin: [{role: admin}]\nauthorization: {}\n", "auth.authz (roles) belongs to claims mode"},
		{"public paths in Cedar mode", oauthBlock + "  publicPaths: [/extra]\nauthorization: {}\n", "auth.publicPaths is not supported in Cedar mode"},
		{"relative policy file", oauthBlock + "authorization: {policyFile: policy.cedar}\n", "must be an absolute path"},
		{"admin without OAuth", "auth: {mode: anonymous}\nauthorization: {allowAnonymousCatalog: true}\nadmin: {enabled: true}\n", "admin.enabled requires auth.mode oauth"},
		{"anonymous without opt-in", "auth: {mode: anonymous}\nauthorization: {}\n", "requires authorization.allowAnonymousCatalog"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := loadAuthzConfig(t, tc.yaml)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestCedarModeRejectsClaims(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := `
sources:
  - name: local
    file: {path: /data/registry.json}
    syncPolicy: {interval: 30m}
    claims: {org: acme}
registries:
  - name: main
    sources: [local]
database: {host: localhost, port: 5432, user: u, database: d}
` + oauthBlock + "authorization: {}\n"
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0600))
	_, err := LoadConfig(WithConfigPath(path))
	require.ErrorContains(t, err, "claims belong to claims mode")
}

//nolint:paralleltest // t.Setenv cannot be used in parallel tests
func TestAuthorizationEnvironmentOverrides(t *testing.T) {
	t.Setenv(envAuthorizationPolicyFile, "/etc/registry/env.cedar")
	t.Setenv(envAdminEnabled, "true")
	cfg, err := loadAuthzConfig(t, oauthBlock)
	require.NoError(t, err)
	assert.Equal(t, AuthorizationModeCedar, cfg.AuthorizationMode(), "the policy file variable selects Cedar")
	assert.Equal(t, "/etc/registry/env.cedar", cfg.Authorization.PolicyFile)
	assert.True(t, cfg.AdminEnabled())

	t.Setenv(envAdminEnabled, "not-a-bool")
	_, err = loadAuthzConfig(t, oauthBlock)
	require.ErrorContains(t, err, envAdminEnabled)
}

//nolint:paralleltest // asserts that unset variables do not change the mode
func TestUnsetAuthorizationEnvironmentKeepsClaimsMode(t *testing.T) {
	cfg, err := loadAuthzConfig(t, "auth: {mode: anonymous}\n")
	require.NoError(t, err)
	assert.Nil(t, cfg.Authorization)
	assert.Nil(t, cfg.Admin)
	assert.Equal(t, AuthorizationModeClaims, cfg.AuthorizationMode())
}

func TestCedarIdentityMappingsValidated(t *testing.T) {
	t.Parallel()
	ok := oauthBlock + `authorization:
  identityMappings:
    - issuer: https://idp.example.com
      groups: {claim: groups, format: string-array}
      scopes: {claim: scope, format: space-delimited}
`
	cfg, err := loadAuthzConfig(t, ok)
	require.NoError(t, err)
	mappings := cfg.Authorization.CedarMappings()
	require.Len(t, mappings, 1)
	assert.Equal(t, "groups", mappings[0].Groups.Claim)
	assert.Equal(t, "space-delimited", string(mappings[0].Scopes.Format))

	for name, yaml := range map[string]string{
		"unknown issuer": oauthBlock + "authorization:\n  identityMappings:\n    - issuer: https://other.example.com\n      groups: {claim: groups, format: string-array}\n",
		"reserved claim": oauthBlock + "authorization:\n  identityMappings:\n    - issuer: https://idp.example.com\n      groups: {claim: sub, format: string-array}\n",
		"unknown format": oauthBlock + "authorization:\n  identityMappings:\n    - issuer: https://idp.example.com\n      groups: {claim: groups, format: csv}\n",
	} {
		_, err := loadAuthzConfig(t, yaml)
		assert.Error(t, err, name)
	}
}
