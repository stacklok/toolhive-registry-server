package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// AuthorizationConfig configures the standalone, restart-loaded Cedar gate.
type AuthorizationConfig struct {
	// Omission evaluates built-in allow-all Cedar; custom files replace it entirely.
	PolicyFile            string            `yaml:"policyFile"`
	AllowAnonymousCatalog bool              `yaml:"allowAnonymousCatalog"`
	IdentityMappings      []IdentityMapping `yaml:"identityMappings"`
}

// IdentityMapping selects attributes from one verified issuer, without fallbacks.
type IdentityMapping struct {
	Issuer string            `yaml:"issuer"`
	Groups *AttributeMapping `yaml:"groups"`
	Scopes *AttributeMapping `yaml:"scopes"`
}

// AttributeMapping defines the exact token claim and its representation.
type AttributeMapping struct {
	Claim  string `yaml:"claim"`
	Format string `yaml:"format"`
}

// AdminConfig controls whether the standalone admin surface is mounted.
type AdminConfig struct {
	Enabled bool `yaml:"enabled"`
}

// ValidateAdmission validates the effective configuration after CLI overrides.
func (c *Config) ValidateAdmission() error {
	if c == nil || c.Auth == nil {
		return fmt.Errorf("auth configuration is required")
	}
	if c.Auth.Mode == "" {
		c.Auth.Mode = AuthModeOAuth
	}
	if err := c.Auth.Validate(c.Auth.InsecureAllowHTTP); err != nil {
		return err
	}
	if c.Auth.PublicPaths != nil || c.publicPathsPresent {
		return fmt.Errorf("auth.publicPaths is not supported")
	}
	if c.Authorization == nil {
		c.Authorization = &AuthorizationConfig{}
	}
	if c.Authorization.PolicyFile != "" && !filepath.IsAbs(c.Authorization.PolicyFile) {
		return fmt.Errorf("authorization.policyFile must be an absolute path when specified")
	}
	if c.Admin.Enabled && c.Auth.Mode != AuthModeOAuth {
		return fmt.Errorf("admin.enabled requires OAuth")
	}
	if err := c.validateAnonymousAdmission(); err != nil {
		return err
	}
	return c.validateIdentityMappings()
}

func (c *Config) validateAnonymousAdmission() error {
	if c.Auth.Mode == AuthModeAnonymous && !c.Authorization.AllowAnonymousCatalog {
		return fmt.Errorf("anonymous mode requires authorization.allowAnonymousCatalog")
	}
	// Legacy list filters treat an absent caller as unfiltered. Until their
	// removal/migration, anonymous admission must not erase that boundary.
	if c.Auth.Authz != nil && (c.Auth.Mode == AuthModeAnonymous || c.Authorization.AllowAnonymousCatalog) {
		return fmt.Errorf("anonymous catalog is unavailable while legacy auth.authz is enabled")
	}
	return nil
}

func (c *Config) validateIdentityMappings() error {
	issuers := map[string]bool{}
	names := map[string]bool{}
	if c.Auth.OAuth != nil {
		for _, p := range c.Auth.OAuth.Providers {
			if issuers[p.IssuerURL] || names[p.Name] {
				return fmt.Errorf("duplicate OAuth issuer or provider")
			}
			issuers[p.IssuerURL], names[p.Name] = true, true
		}
	}
	mappings := map[string]bool{}
	for _, m := range c.Authorization.IdentityMappings {
		if !issuers[m.Issuer] || mappings[m.Issuer] {
			return fmt.Errorf("identity mapping requires a unique configured issuer")
		}
		mappings[m.Issuer] = true
		for _, a := range []*AttributeMapping{m.Groups, m.Scopes} {
			if err := validateAttributeMapping(a); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateAttributeMapping(a *AttributeMapping) error {
	if a == nil {
		return nil
	}
	if len(a.Claim) == 0 || len(a.Claim) > 128 || strings.TrimSpace(a.Claim) != a.Claim {
		return fmt.Errorf("invalid mapped claim name")
	}
	switch a.Claim {
	case "iss", "sub", "authenticated", "issuer", "subject":
		return fmt.Errorf("reserved identity mapping")
	}
	if a.Format != "string-array" && a.Format != "space-delimited" {
		return fmt.Errorf("invalid mapped claim format")
	}
	return nil
}
