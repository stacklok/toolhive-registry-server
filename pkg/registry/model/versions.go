package model

import (
	"strings"

	"github.com/Masterminds/semver/v3"
)

// CompareVersions defines a total order over exact stored version strings.
// Versions accepted by Masterminds semver rank above unparsable strings;
// semantic precedence wins within that group, with raw spelling breaking ties.
// Unparsable strings compare lexically. No version is normalized or rejected.
// The result is negative for a < b, zero only for identical strings, and
// positive for a > b.
func CompareVersions(a, b string) int {
	av, aerr := semver.NewVersion(a)
	bv, berr := semver.NewVersion(b)
	switch {
	case aerr == nil && berr != nil:
		return 1
	case aerr != nil && berr == nil:
		return -1
	case aerr == nil && berr == nil:
		if n := av.Compare(bv); n != 0 {
			return n
		}
	}
	return strings.Compare(a, b)
}
