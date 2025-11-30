// Package service provides the business logic for the MCP registry API.
//
// This file contains utility functions for server name prefixing in multi-registry
// environments. When querying across multiple registries (aggregated queries),
// server names are prefixed with the registry name to ensure uniqueness and
// enable proper server identification.
package service

// PrefixServerName adds a registry name prefix to a server name using a dot delimiter.
// This creates a fully qualified server name in the format "registryName.serverName".
//
// Example:
//
//	PrefixServerName("partner-a", "io.github.user/server") returns "partner-a.io.github.user/server"
func PrefixServerName(registryName, serverName string) string {
	return registryName + "." + serverName
}

// ShouldPrefixNames determines whether server names should be prefixed with registry names.
// Returns true when registryName is nil, indicating an aggregated query across all registries.
// Returns false when registryName is non-nil, indicating a query to a specific registry.
//
// In aggregated queries, prefixing ensures server names are unique across different registries
// that may contain servers with the same name.
func ShouldPrefixNames(registryName *string) bool {
	return registryName == nil
}
