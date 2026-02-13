package authz

import (
	"net/http"
	"strings"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// Action aliases from config for convenience within the authz package.
const (
	ActionRead  = config.ActionRead
	ActionWrite = config.ActionWrite
	ActionAdmin = config.ActionAdmin
)

// RouteAction determines the required Cedar action based on HTTP method and path.
func RouteAction(method, path string) string {
	// Extension API admin operations (registry lifecycle management)
	if isExtensionRegistryMutation(method, path) {
		return ActionAdmin
	}

	// Registry v0.1 write operations
	if isRegistryWrite(method, path) {
		return ActionWrite
	}

	// All GET requests are reads
	if method == http.MethodGet {
		return ActionRead
	}

	// Default: require admin for unknown mutating operations
	return ActionAdmin
}

// isExtensionRegistryMutation checks if the request is a mutation on extension registries.
// PUT/DELETE /extension/v0/registries/{name} are admin operations.
func isExtensionRegistryMutation(method, path string) bool {
	if method != http.MethodPut && method != http.MethodDelete {
		return false
	}
	// Match /extension/v0/registries/{name} but NOT deeper paths like .../servers/...
	if !strings.HasPrefix(path, "/extension/v0/registries/") {
		return false
	}
	// Get the part after /extension/v0/registries/
	remainder := strings.TrimPrefix(path, "/extension/v0/registries/")
	// If there's no slash after the name, it's a registry mutation
	// If there IS a slash (e.g., /servers/...), it's a deeper operation
	return !strings.Contains(remainder, "/")
}

// isRegistryWrite checks if the request is a write operation on registry servers.
// POST .../publish and DELETE .../versions/{version} are write operations.
func isRegistryWrite(method, path string) bool {
	if method == http.MethodPost && strings.Contains(path, "/v0.1/publish") {
		return true
	}
	if method == http.MethodDelete && strings.Contains(path, "/v0.1/servers/") {
		return true
	}
	return false
}
