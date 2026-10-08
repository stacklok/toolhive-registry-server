// Package validators preserves existing validation entry points for the HTTP layer.
package validators

import "github.com/stacklok/toolhive-registry-server/pkg/registry/formats"

// ValidateServerName validates an MCP server name and returns its trimmed form.
func ValidateServerName(name string) (string, error) {
	return formats.ValidateServerName(name)
}

// IsValidServerName reports whether an MCP server name is valid.
func IsValidServerName(name string) bool {
	return formats.IsValidServerName(name)
}
