package auth

import (
	"strings"

	"github.com/stacklok/toolhive/pkg/auth"
)

// providerConfig holds configuration for a single token validation provider
type providerConfig struct {
	// Name is a human-readable identifier for this provider
	Name string

	// IssuerURL is the OIDC issuer URL for this provider
	IssuerURL string

	// ValidatorConfig is the ToolHive TokenValidator configuration
	ValidatorConfig auth.TokenValidatorConfig
}

// IsProtectedPath checks if a path requires authentication
func IsProtectedPath(path string, protectedPaths, publicPaths []string) bool {
	// First check public paths
	for _, publicPath := range publicPaths {
		if strings.HasPrefix(path, publicPath) {
			return false
		}
	}

	// Check configured protected paths
	for _, protectedPath := range protectedPaths {
		if strings.HasPrefix(path, protectedPath) {
			return true
		}
	}

	return false
}
