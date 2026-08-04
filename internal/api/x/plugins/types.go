// Package plugins provides API types and handlers for the dev.toolhive/plugins
// extension endpoints.
package plugins

import thvregistry "github.com/stacklok/toolhive-core/registry/types"

// ListPluginsQuery holds parsed query parameters for GET /plugins (list).
type ListPluginsQuery struct {
	Search string
	Status string // comma-separated for IN filtering, e.g. "active,deprecated"
	Limit  int    // default 50, max 100
	Cursor string
}

// PluginListMetadata is the metadata object in list responses.
type PluginListMetadata struct {
	Count      int    `json:"count"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// PluginListResponse is the response for GET /plugins (list).
type PluginListResponse struct {
	Plugins  []thvregistry.Plugin `json:"plugins"`
	Metadata PluginListMetadata   `json:"metadata"`
}
