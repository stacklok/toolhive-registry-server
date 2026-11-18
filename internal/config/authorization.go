package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// AuthorizationMode is how a deployment authorizes requests. A deployment
// uses exactly one mode for its whole life: the server records the mode in
// the database on first start and refuses to start with the other one.
type AuthorizationMode string

const (
	// AuthorizationModeClaims is v1.5.4's model: roles from auth.authz plus
	// per-resource claims. It is the mode when no authorization block is set.
	AuthorizationModeClaims AuthorizationMode = "claims"
	// AuthorizationModeCedar authorizes each route with a Cedar policy. It is
	// selected by the authorization block.
	AuthorizationModeCedar AuthorizationMode = "cedar"
)

// AuthorizationConfig configures Cedar mode. Its presence selects Cedar.
type AuthorizationConfig struct {
	// PolicyFile is an absolute path to a Cedar policy. When empty, the
	// built-in policy permits every action to every caller that passes
	// authentication; supply a policy before enabling admin routes.
	PolicyFile string `yaml:"policyFile,omitempty"`
	// AllowAnonymousCatalog lets unauthenticated callers read registries the
	// policy permits. Requests that carry a token are always validated.
	AllowAnonymousCatalog bool `yaml:"allowAnonymousCatalog,omitempty"`
	// IdentityMappings choose, per OAuth issuer, the token claims that become
	// the Cedar principal's groups and scopes.
	IdentityMappings []IdentityMapping `yaml:"identityMappings,omitempty"`
}

// IdentityMapping selects principal attributes from one OAuth issuer's tokens.
type IdentityMapping struct {
	Issuer string            `yaml:"issuer"`
	Groups *AttributeMapping `yaml:"groups,omitempty"`
	Scopes *AttributeMapping `yaml:"scopes,omitempty"`
}

// AttributeMapping names a token claim and its format: "string-array" or
// "space-delimited".
type AttributeMapping struct {
	Claim  string `yaml:"claim"`
	Format string `yaml:"format"`
}

// AdminConfig controls the admin API in Cedar mode. Claims mode always serves
// /v1 and rejects this block.
type AdminConfig struct {
	// Enabled mounts /v1 in Cedar mode. It requires OAuth.
	Enabled bool `yaml:"enabled"`
}

// Environment overrides for Cedar mode; see applyAuthorizationEnv.
const (
	envAuthorizationPolicyFile     = EnvPrefix + "_AUTHORIZATION_POLICYFILE"
	envAuthorizationAllowAnonymous = EnvPrefix + "_AUTHORIZATION_ALLOWANONYMOUSCATALOG"
	envAdminEnabled                = EnvPrefix + "_ADMIN_ENABLED"
)

// AuthorizationMode reports the mode this configuration selects.
func (c *Config) AuthorizationMode() AuthorizationMode {
	if c.Authorization != nil {
		return AuthorizationModeCedar
	}
	return AuthorizationModeClaims
}

// AdminEnabled reports whether /v1 is served: always in claims mode, and in
// Cedar mode only when admin.enabled is set.
func (c *Config) AdminEnabled() bool {
	if c.AuthorizationMode() == AuthorizationModeClaims {
		return true
	}
	return c.Admin != nil && c.Admin.Enabled
}

// applyAuthorizationEnv applies the Cedar-mode environment overrides. They are
// read directly rather than through viper defaults so that an unset variable
// never makes the authorization block present, which would select Cedar mode.
func (c *Config) applyAuthorizationEnv() error {
	if v, ok := os.LookupEnv(envAuthorizationPolicyFile); ok {
		c.ensureAuthorization().PolicyFile = v
	}
	if v, ok := os.LookupEnv(envAuthorizationAllowAnonymous); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("%s: %w", envAuthorizationAllowAnonymous, err)
		}
		c.ensureAuthorization().AllowAnonymousCatalog = b
	}
	if v, ok := os.LookupEnv(envAdminEnabled); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("%s: %w", envAdminEnabled, err)
		}
		if c.Admin == nil {
			c.Admin = &AdminConfig{}
		}
		c.Admin.Enabled = b
	}
	return nil
}

func (c *Config) ensureAuthorization() *AuthorizationConfig {
	if c.Authorization == nil {
		c.Authorization = &AuthorizationConfig{}
	}
	return c.Authorization
}

// ValidateAuthorizationMode keeps each mode's settings out of the other. Load
// runs it; run it again after overriding auth settings (such as the
// --auth-mode flag), because Cedar mode's rules depend on them.
func (c *Config) ValidateAuthorizationMode() error {
	if c.AuthorizationMode() == AuthorizationModeClaims {
		if c.Admin != nil {
			return errors.New("admin applies to Cedar mode only; claims mode always serves /v1 " +
				"(add an authorization block to use Cedar mode)")
		}
		return nil
	}
	return c.validateCedarMode()
}

func (c *Config) validateCedarMode() error {
	if c.Auth.Authz != nil {
		return errors.New("auth.authz (roles) belongs to claims mode; Cedar mode (authorization) " +
			"authorizes with the policy instead")
	}
	if len(c.Auth.PublicPaths) > 0 {
		return errors.New("auth.publicPaths is not supported in Cedar mode")
	}
	for i, src := range c.Sources {
		if len(src.Claims) > 0 {
			return fmt.Errorf("source[%d] (%s): claims belong to claims mode and are not used in Cedar mode", i, src.Name)
		}
	}
	for i, reg := range c.Registries {
		if len(reg.Claims) > 0 {
			return fmt.Errorf("registry[%d] (%s): claims belong to claims mode and are not used in Cedar mode", i, reg.Name)
		}
	}
	a := c.Authorization
	if a.PolicyFile != "" && !filepath.IsAbs(a.PolicyFile) {
		return errors.New("authorization.policyFile must be an absolute path")
	}
	if c.AdminEnabled() && c.Auth.Mode != AuthModeOAuth {
		return errors.New("admin.enabled requires auth.mode oauth")
	}
	if c.Auth.Mode == AuthModeAnonymous && !a.AllowAnonymousCatalog {
		return errors.New("auth.mode anonymous requires authorization.allowAnonymousCatalog in Cedar mode")
	}
	return c.validateIdentityMappings()
}

// validateIdentityMappings checks the mappings against the configured OAuth
// issuers; the claim-level rules are the Cedar authorizer's own.
func (c *Config) validateIdentityMappings() error {
	if len(c.Authorization.IdentityMappings) == 0 {
		return nil
	}
	return validateCedarMappings(c.oauthIssuers(), c.Authorization.IdentityMappings)
}

func (c *Config) oauthIssuers() []string {
	if c.Auth == nil || c.Auth.OAuth == nil {
		return nil
	}
	issuers := make([]string, 0, len(c.Auth.OAuth.Providers))
	for _, p := range c.Auth.OAuth.Providers {
		issuers = append(issuers, p.IssuerURL)
	}
	return issuers
}
