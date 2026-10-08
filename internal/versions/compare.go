package versions

import "github.com/stacklok/toolhive-registry-server/pkg/registry/model"

// IsNewerVersion reports whether newVersion ranks strictly above oldVersion
// under the shared total order used by all persistence implementations.
func IsNewerVersion(newVersion, oldVersion string) bool {
	return model.CompareVersions(newVersion, oldVersion) > 0
}
