package service

import "github.com/stacklok/toolhive-registry-server/pkg/registry/model"

// Plugin and its related types are retained as aliases for existing service callers.
type Plugin = model.Plugin

// PluginRepository is retained for service callers.
type PluginRepository = model.PluginRepository

// PluginPackage is retained for service callers.
type PluginPackage = model.PluginPackage

// PluginIcon is retained for service callers.
type PluginIcon = model.PluginIcon

// ListPluginsResult contains the result of a ListPlugins operation with pagination.
type ListPluginsResult struct {
	Plugins    []*Plugin `json:"plugins"`
	NextCursor string    `json:"-"`
}
