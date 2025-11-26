// Package registry contains shared types, utilities and constants for registry operations
package registry

const (
	// UpstreamRegistrySchemaURL is the JSON schema URL for upstream registry validation
	UpstreamRegistrySchemaURL = "https://raw.githubusercontent.com/stacklok/toolhive/main/" +
		"pkg/registry/data/upstream-registry.schema.json"

	// UpstreamRegistryVersion is the default upstream registry schema version
	UpstreamRegistryVersion = "1.0.0"
)
