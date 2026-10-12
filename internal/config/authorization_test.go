package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdmissionEffectiveConfiguration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*Config)
		valid  bool
	}{
		{"private", func(*Config) {}, true},
		{"missing policy", func(c *Config) { c.Authorization = nil }, false},
		{"relative policy", func(c *Config) { c.Authorization.PolicyFile = "policy.cedar" }, false},
		{"admin anonymous", func(c *Config) { c.Admin.Enabled = true; c.Auth.Mode = AuthModeAnonymous }, false},
		{"bad CLI override", func(c *Config) { c.Auth.Mode = "invalid" }, false},
		{"public paths empty", func(c *Config) { c.Auth.PublicPaths = []string{} }, false},
		{"public paths root", func(c *Config) { c.Auth.PublicPaths = []string{"/"} }, false},
		{"duplicate issuer", func(c *Config) { c.Auth.OAuth.Providers = append(c.Auth.OAuth.Providers, c.Auth.OAuth.Providers[0]) }, false},
		{"duplicate mapping", func(c *Config) {
			c.Authorization.IdentityMappings = append(c.Authorization.IdentityMappings, c.Authorization.IdentityMappings[0])
		}, false},
		{"unknown issuer mapping", func(c *Config) { c.Authorization.IdentityMappings[0].Issuer = "https://other" }, false},
		{"reserved mapping", func(c *Config) { c.Authorization.IdentityMappings[0].Groups.Claim = "iss" }, false},
		{"bad mapping format", func(c *Config) { c.Authorization.IdentityMappings[0].Groups.Format = "guess" }, false},
		{"anonymous without opt-in", func(c *Config) { c.Auth.Mode = AuthModeAnonymous }, false},
		{"anonymous opt-in", func(c *Config) { c.Auth.Mode = AuthModeAnonymous; c.Authorization.AllowAnonymousCatalog = true }, true},
		{"anonymous cannot bypass legacy claims", func(c *Config) {
			c.Authorization.AllowAnonymousCatalog = true
			c.Auth.Authz = &AuthzConfig{}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := &Config{Auth: &AuthConfig{Mode: AuthModeOAuth, OAuth: &OAuthConfig{Providers: []OAuthProviderConfig{{Name: "one", IssuerURL: "https://issuer.example", Audience: "registry"}}}}, Authorization: &AuthorizationConfig{PolicyFile: "/etc/registry/policy.cedar", IdentityMappings: []IdentityMapping{{Issuer: "https://issuer.example", Groups: &AttributeMapping{Claim: "groups", Format: "string-array"}}}}}
			tc.change(cfg)
			err := cfg.ValidateAdmission()
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestAdmissionOAuthDefault(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`sources:
 - name: managed
   managed: {}
registries:
 - name: default
   sources: [managed]
database:
 host: localhost
 port: 5432
 user: registry
 database: registry
 sslMode: disable
auth:
 oauth:
  providers:
   - name: company
     issuerUrl: https://id.example.org
     audience: registry-api
authorization:
 policyFile: /etc/registry/policy.cedar
`), 0600))
	cfg, err := LoadConfig(WithConfigPath(path))
	require.NoError(t, err)
	require.Equal(t, AuthModeOAuth, cfg.Auth.Mode)
	require.False(t, cfg.Admin.Enabled)
	require.False(t, cfg.Authorization.AllowAnonymousCatalog)
	require.NoError(t, cfg.ValidateAdmission())
}

func TestPublicPathsPresenceRejected(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"[]", "null", "", "[\"/\"]"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.yaml")
			yaml := `sources:
 - name: managed
   managed: {}
registries:
 - name: default
   sources: [managed]
database:
 host: localhost
 port: 5432
 user: registry
 database: registry
 sslMode: disable
auth:
 mode: anonymous
 publicPaths: ` + value + `
authorization:
 policyFile: /etc/registry/policy.cedar
 allowAnonymousCatalog: true
`
			require.NoError(t, os.WriteFile(path, []byte(yaml), 0600))
			cfg, err := LoadConfig(WithConfigPath(path))
			require.NoError(t, err)
			require.ErrorContains(t, cfg.ValidateAdmission(), "auth.publicPaths")
		})
	}
}
