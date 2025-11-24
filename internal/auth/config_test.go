package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsPublicPath(t *testing.T) {
	t.Parallel()

	standardPublicPaths := []string{"/health", "/docs", "/swagger"}

	tests := []struct {
		name        string
		path        string
		publicPaths []string
		want        bool
	}{
		// Basic functionality
		{"exact match", "/health", standardPublicPaths, true},
		{"subpath match", "/docs/api/v1", standardPublicPaths, true},
		{"no match", "/api/v1/servers", standardPublicPaths, false},
		{"empty public paths", "/any", []string{}, false},
		{"nil public paths", "/health", nil, false},

		// Path traversal attacks (security critical)
		{"traversal to protected", "/health/../v0/servers", standardPublicPaths, false},
		{"traversal multiple levels", "/docs/../../api/secrets", standardPublicPaths, false},
		{"traversal stays in public", "/docs/v1/../v2", standardPublicPaths, true},

		// Double encoding attacks
		{"encoded path separators", "/swagger/..%2f..%2fv0/servers", standardPublicPaths, false},

		// Unintended prefix matches (security critical)
		{"healthcheck not health", "/healthcheck", standardPublicPaths, false},
		{"swaggerui not swagger", "/swaggerui", standardPublicPaths, false},

		// Correct segment boundaries
		{"health/check matches", "/health/check", standardPublicPaths, true},
		{"trailing slash", "/health/", standardPublicPaths, true},

		// Path normalization
		{"double slash", "//health", standardPublicPaths, true},
		{"dot reference", "/./docs/api", standardPublicPaths, true},

		// Root path special case
		{"root exact", "/", []string{"/"}, true},
		{"root makes all public", "/api/servers", []string{"/"}, true},

		// Case sensitivity (URLs are case-sensitive)
		{"case sensitive", "/Health", standardPublicPaths, false},

		// Combined attack
		{"traversal with normalization", "//health/..//api", standardPublicPaths, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsPublicPath(tt.path, tt.publicPaths)
			assert.Equal(t, tt.want, got, "path=%q, publicPaths=%v", tt.path, tt.publicPaths)
		})
	}
}
