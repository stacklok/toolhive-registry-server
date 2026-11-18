package config

import "github.com/stacklok/toolhive-registry-server/pkg/registry/cedar"

// CedarMappings converts the configured identity mappings to the Cedar
// authorizer's types.
func (a *AuthorizationConfig) CedarMappings() []cedar.Mapping {
	if a == nil {
		return nil
	}
	out := make([]cedar.Mapping, 0, len(a.IdentityMappings))
	for _, m := range a.IdentityMappings {
		out = append(out, cedar.Mapping{
			Issuer: m.Issuer, Groups: cedarAttribute(m.Groups), Scopes: cedarAttribute(m.Scopes),
		})
	}
	return out
}

func cedarAttribute(m *AttributeMapping) *cedar.AttributeMapping {
	if m == nil {
		return nil
	}
	return &cedar.AttributeMapping{Claim: m.Claim, Format: cedar.Format(m.Format)}
}

// validateCedarMappings applies the Cedar authorizer's own mapping rules.
func validateCedarMappings(issuers []string, mappings []IdentityMapping) error {
	return cedar.ValidateMappings(issuers, (&AuthorizationConfig{IdentityMappings: mappings}).CedarMappings())
}
