// Package validators provides validation functions for MCP Registry Server entities.
package validators

import "github.com/stacklok/toolhive-registry-server/pkg/registry/formats"

// ValidateServerName validates an MCP server name against the MCP Registry
// specification and returns its trimmed form.
func ValidateServerName(name string) (string, error) {
	return formats.ValidateServerName(name)
}

// IsValidServerName reports whether a server name is valid according to the
// MCP Registry specification.
func IsValidServerName(name string) bool {
	return formats.IsValidServerName(name)
}
