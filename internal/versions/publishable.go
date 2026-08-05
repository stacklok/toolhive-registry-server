package versions

import (
	"strings"

	"github.com/Masterminds/semver/v3"
)

// maxVersionLength mirrors the `maxLength` constraint on `version` in the upstream
// server.json schema.
const maxVersionLength = 255

// IsPublishable reports whether v is usable as a registry entry version.
//
// The gate is "a three-part semantic version", matching the official registry's own
// semantic-version check (major.minor.patch, optional prerelease, no leading zeros).
// Looser versions are legal per the JSON schema, which constrains only length, but
// they sort unpredictably: the official registry marks anything it cannot parse as
// "latest" regardless of ordering, aggregators are told to fall back to publication
// timestamps, and IsNewerVersion degrades to lexicographic comparison. Restricting
// generated versions to the parseable set keeps latest-version resolution consistent
// across all three.
//
// A leading "v" is accepted, since the schema lists prefixed versions as allowed.
// Callers should keep the value as written rather than stripping the prefix — the API
// matches versions exactly, and an entry's version should stay equal to the package
// version it was derived from.
//
// Build metadata is rejected: semver comparison ignores it, so two versions differing
// only in metadata would be distinct rows that sort equal, and "+" decodes as a space
// in `?version=` for clients that do not percent-encode it.
func IsPublishable(v string) bool {
	if v == "" || len(v) > maxVersionLength {
		return false
	}
	if strings.ContainsRune(v, '+') {
		return false
	}
	_, err := semver.StrictNewVersion(strings.TrimPrefix(v, "v"))
	return err == nil
}
