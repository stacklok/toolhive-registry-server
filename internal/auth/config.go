package auth

import (
	"path"
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

// IsPublicPath checks if a path should bypass authentication.
// It performs secure path matching by:
// 1. Rejecting paths with encoded path separators to prevent double-encoding attacks
// 2. Normalizing the path to prevent traversal attacks (e.g., /health/../v0/servers)
// 3. Using segment-aware matching so /health matches /health and /health/check but NOT /healthcheck
func IsPublicPath(requestPath string, publicPaths []string) bool {
	// Reject paths containing encoded path separators (double-encoding attack)
	// %2f = /, %2F = /, %2e = ., %2E = .
	lowerPath := strings.ToLower(requestPath)
	if strings.Contains(lowerPath, "%2f") || strings.Contains(lowerPath, "%2e") {
		return false
	}

	// Normalize the path to handle traversals and double slashes
	cleanPath := path.Clean(requestPath)

	// Ensure path starts with /
	if !strings.HasPrefix(cleanPath, "/") {
		cleanPath = "/" + cleanPath
	}

	for _, publicPath := range publicPaths {
		// Clean the public path as well for consistent comparison
		cleanPublicPath := path.Clean(publicPath)
		if !strings.HasPrefix(cleanPublicPath, "/") {
			cleanPublicPath = "/" + cleanPublicPath
		}

		// Special case: root path "/" makes everything public
		if cleanPublicPath == "/" {
			return true
		}

		// Check for exact match
		if cleanPath == cleanPublicPath {
			return true
		}

		// Check for prefix match with segment boundary
		// The request path must start with the public path followed by a /
		// This ensures /health matches /health/check but NOT /healthcheck
		if strings.HasPrefix(cleanPath, cleanPublicPath+"/") {
			return true
		}
	}
	return false
}
