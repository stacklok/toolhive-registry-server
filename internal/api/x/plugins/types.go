// Package plugins provides API types and handlers for the dev.toolhive/plugins
// extension endpoints.
package plugins

import "github.com/stacklok/toolhive-registry-server/pkg/registry/formats"

// ListPluginsQuery holds parsed query parameters for GET /plugins (list).
type ListPluginsQuery struct {
	Search string
	Status string // comma-separated for IN filtering, e.g. "active,deprecated"
	Limit  int    // default 50, max 100
	Cursor string
}

// PluginListMetadata is the metadata object in list responses.
type PluginListMetadata = formats.ExtensionMetadata

// PluginListResponse is the response for GET /plugins (list).
type PluginListResponse = formats.PluginListResponse
