package versions

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsNewerVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		newVersion string
		oldVersion string
		expected   bool
	}{
		// Valid semver comparisons
		{name: "newer major version", newVersion: "2.0.0", oldVersion: "1.0.0", expected: true},
		{name: "newer minor version", newVersion: "1.2.0", oldVersion: "1.1.0", expected: true},
		{name: "newer patch version", newVersion: "1.0.2", oldVersion: "1.0.1", expected: true},
		{name: "older major version", newVersion: "1.0.0", oldVersion: "2.0.0", expected: false},
		{name: "older minor version", newVersion: "1.1.0", oldVersion: "1.2.0", expected: false},
		{name: "older patch version", newVersion: "1.0.1", oldVersion: "1.0.2", expected: false},
		{name: "equal versions", newVersion: "1.0.0", oldVersion: "1.0.0", expected: false},
		{name: "prerelease vs release", newVersion: "1.0.0", oldVersion: "1.0.0-alpha", expected: true},
		{name: "release vs prerelease", newVersion: "1.0.0-alpha", oldVersion: "1.0.0", expected: false},
		{name: "newer prerelease", newVersion: "1.0.0-beta", oldVersion: "1.0.0-alpha", expected: true},
		// Fallback to string comparison for non-semver
		{name: "non-semver string comparison newer", newVersion: "version-b", oldVersion: "version-a", expected: true},
		{name: "non-semver string comparison older", newVersion: "version-a", oldVersion: "version-b", expected: false},
		{name: "non-semver equal", newVersion: "custom-v1", oldVersion: "custom-v1", expected: false},
		{name: "mixed semver and non-semver - semver first", newVersion: "1.0.0", oldVersion: "invalid-version", expected: false},
		{name: "mixed semver and non-semver - non-semver first", newVersion: "invalid-version", oldVersion: "1.0.0", expected: true},
		{name: "empty new version", newVersion: "", oldVersion: "1.0.0", expected: false},
		{name: "empty old version", newVersion: "1.0.0", oldVersion: "", expected: true},
		{name: "both empty", newVersion: "", oldVersion: "", expected: false},
		// Edge cases with v prefix
		{name: "v prefix newer", newVersion: "v2.0.0", oldVersion: "v1.0.0", expected: true},
		{name: "v prefix older", newVersion: "v1.0.0", oldVersion: "v2.0.0", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := IsNewerVersion(tt.newVersion, tt.oldVersion)
			assert.Equal(t, tt.expected, result)
		})
	}
}
