package versions

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsPublishable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		version  string
		expected bool
	}{
		// Three-part semantic versions, the recommended form.
		{name: "plain semver", version: "1.4.2", expected: true},
		{name: "zero version", version: "0.0.1", expected: true},
		{name: "prerelease", version: "2.1.0-alpha", expected: true},
		{name: "dotted prerelease", version: "1.0.0-beta.1", expected: true},
		{name: "release candidate", version: "3.0.0-rc.2", expected: true},
		{name: "image variant reads as a prerelease", version: "1.2.3-alpine", expected: true},
		{name: "semantic date", version: "2025.11.25", expected: true},

		// A leading "v" is allowed by the upstream schema.
		{name: "v prefix", version: "v1.2.3", expected: true},
		{name: "v prefix with prerelease", version: "v1.2.3-alpine3.19", expected: true},

		// Fewer than three parts: allowed by the schema, but the official registry
		// does not treat these as semantic and they sort unpredictably.
		{name: "two-part version", version: "1.2", expected: false},
		{name: "single-part version", version: "1", expected: false},
		{name: "v prefix, two parts", version: "v1.0", expected: false},
		{name: "four-part version", version: "1.2.3.4", expected: false},

		// Leading zeros are not valid semver.
		{name: "leading zero in patch", version: "2025.06.18", expected: false},
		{name: "leading zero in major", version: "01.2.3", expected: false},

		// Build metadata is ignored by semver comparison and breaks `?version=`.
		{name: "build metadata", version: "1.2.3+build.5", expected: false},

		// Reserved and range-like values.
		{name: "reserved latest", version: "latest", expected: false},
		{name: "caret range", version: "^1.2.3", expected: false},
		{name: "tilde range", version: "~1.2.3", expected: false},
		{name: "comparator range", version: ">=1.2.3", expected: false},
		{name: "x range", version: "1.x", expected: false},
		{name: "wildcard range", version: "1.2.*", expected: false},

		// Values that would be unusable as an entry version or URL path segment.
		{name: "empty", version: "", expected: false},
		{name: "whitespace only", version: "   ", expected: false},
		{name: "internal whitespace", version: "1.0.0 beta", expected: false},
		{name: "untrimmed", version: " 1.0.0 ", expected: false},
		{name: "image digest", version: "sha256:abc123", expected: false},
		{name: "path separator", version: "5000/my-image", expected: false},
		{name: "channel name", version: "stable", expected: false},
		{name: "commit tag", version: "sha-a1b2c3d", expected: false},
		{name: "non-semver scheme", version: "REL-2024-06", expected: false},
		{name: "underscore", version: "1.2.3_1", expected: false},

		// Length limit from the upstream schema's maxLength on `version`.
		{name: "at the length limit", version: "1.0.0-" + strings.Repeat("a", maxVersionLength-6), expected: true},
		{name: "over the length limit", version: "1.0.0-" + strings.Repeat("a", maxVersionLength-5), expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.expected, IsPublishable(tt.version))
		})
	}
}

// TestIsPublishableImpliesOrderable guards the reason the gate exists: every accepted
// version must be comparable by IsNewerVersion without falling back to lexicographic
// comparison, so latest-version resolution stays meaningful.
func TestIsPublishableImpliesOrderable(t *testing.T) {
	t.Parallel()

	ordered := []string{"0.9.0", "1.0.0-alpha", "1.0.0-beta.1", "1.0.0", "v1.0.1", "1.2.3", "2025.11.25"}

	for i, v := range ordered {
		require := IsPublishable(v)
		assert.True(t, require, "%q should be publishable", v)

		if i > 0 {
			prev := ordered[i-1]
			assert.True(t, IsNewerVersion(v, prev), "%q should sort after %q", v, prev)
			assert.False(t, IsNewerVersion(prev, v), "%q should not sort after %q", prev, v)
		}
	}
}
