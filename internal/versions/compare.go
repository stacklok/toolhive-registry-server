package versions

import "github.com/Masterminds/semver/v3"

// IsNewerVersion reports whether newVersion is strictly greater than oldVersion.
// It uses semantic versioning for comparison when both strings are valid semver,
// and falls back to lexicographic string comparison otherwise.
func IsNewerVersion(newVersion, oldVersion string) bool {
	newSemver, errNew := semver.NewVersion(newVersion)
	oldSemver, errOld := semver.NewVersion(oldVersion)

	if errNew != nil || errOld != nil {
		// Fallback to string comparison if semver parsing fails
		return newVersion > oldVersion
	}

	return newSemver.GreaterThan(oldSemver)
}
