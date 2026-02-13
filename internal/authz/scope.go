package authz

import (
	"strings"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// ExtractScopes extracts OAuth scopes from JWT claims.
// Handles both "scope" (space-separated string per RFC 6749) and
// "scp" (string array, common in Azure AD/Auth0) claim formats.
func ExtractScopes(claims map[string]any) []string {
	// Try "scope" claim (RFC 6749 - space-separated string)
	if scopeStr, ok := claims["scope"].(string); ok && scopeStr != "" {
		return strings.Fields(scopeStr)
	}

	// Try "scp" claim (array format, Azure AD / Auth0)
	if scpArr, ok := claims["scp"].([]any); ok {
		scopes := make([]string, 0, len(scpArr))
		for _, s := range scpArr {
			if str, ok := s.(string); ok {
				scopes = append(scopes, str)
			}
		}
		return scopes
	}

	return nil
}

// MapScopesToActions maps OAuth scopes to granted authorization actions
// using the provided scope mapping configuration.
func MapScopesToActions(scopes []string, mapping []config.ScopeMappingEntry) []string {
	actionSet := make(map[string]bool)

	for _, scope := range scopes {
		for _, entry := range mapping {
			if entry.Scope == scope {
				for _, action := range entry.Actions {
					actionSet[action] = true
				}
			}
		}
	}

	actions := make([]string, 0, len(actionSet))
	for action := range actionSet {
		actions = append(actions, action)
	}
	return actions
}
