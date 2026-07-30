// Package service defines plugin-related types returned by the service layer
// and mapping from sqlc (database) row types.
//
// Plugins mirror skills but omit `compatibility` and `allowed_tools`, which
// are skill-only fields with no plugin analogue.
package service

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
)

// PluginRepository represents source repository metadata at the service layer.
type PluginRepository struct {
	URL  string `json:"url,omitempty"`
	Type string `json:"type,omitempty"`
}

// PluginPackage represents a package for a plugin at the service layer.
type PluginPackage struct {
	RegistryType string `json:"registryType"`
	Identifier   string `json:"identifier,omitempty"`
	Digest       string `json:"digest,omitempty"`
	MediaType    string `json:"mediaType,omitempty"`
	URL          string `json:"url,omitempty"`
	Ref          string `json:"ref,omitempty"`
	Commit       string `json:"commit,omitempty"`
	Subfolder    string `json:"subfolder,omitempty"`
}

// PluginIcon represents a display icon for a plugin at the service layer.
type PluginIcon struct {
	Src   string `json:"src"`
	Size  string `json:"size,omitempty"`
	Type  string `json:"type,omitempty"`
	Label string `json:"label,omitempty"`
}

// Plugin is a single plugin returned by ListPlugins, ListPluginVersions,
// GetPluginVersion, GetLatestPluginVersion, and PublishPlugin.
type Plugin struct {
	ID          string            `json:"id,omitempty"`
	Namespace   string            `json:"namespace"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Version     string            `json:"version"`
	Status      string            `json:"status,omitempty"`
	Title       string            `json:"title,omitempty"`
	License     string            `json:"license,omitempty"`
	Repository  *PluginRepository `json:"repository,omitempty"`
	Icons       []PluginIcon      `json:"icons,omitempty"`
	Packages    []PluginPackage   `json:"packages,omitempty"`
	Metadata    map[string]any    `json:"metadata,omitempty"`
	Meta        map[string]any    `json:"_meta,omitempty"`
	IsLatest    bool              `json:"isLatest,omitempty"`
	CreatedAt   time.Time         `json:"createdAt,omitempty"`
	UpdatedAt   time.Time         `json:"updatedAt,omitempty"`
}

// ListPluginsResult contains the result of a ListPlugins operation with pagination.
type ListPluginsResult struct {
	Plugins    []*Plugin `json:"plugins"`
	NextCursor string    `json:"-"`
}

// pluginRow holds the common shape of sqlc plugin list/get rows for mapping.
type pluginRow struct {
	ID              uuid.UUID
	Name            string
	Version         string
	IsLatest        bool
	CreatedAt       *time.Time
	UpdatedAt       *time.Time
	Description     *string
	Title           *string
	PluginVersionID uuid.UUID
	Namespace       string
	Status          sqlc.PluginStatus
	License         *string
	Repository      []byte
	Icons           []byte
	Metadata        []byte
	ExtensionMeta   []byte
}

func rowToPlugin(r pluginRow) *Plugin {
	resp := &Plugin{
		ID:        r.ID.String(),
		Namespace: r.Namespace,
		Name:      r.Name,
		Version:   r.Version,
		IsLatest:  r.IsLatest,
		Status:    string(r.Status),
	}
	if r.Description != nil {
		resp.Description = *r.Description
	}
	if r.Title != nil {
		resp.Title = *r.Title
	}
	if r.License != nil {
		resp.License = *r.License
	}
	if r.CreatedAt != nil {
		resp.CreatedAt = *r.CreatedAt
	}
	if r.UpdatedAt != nil {
		resp.UpdatedAt = *r.UpdatedAt
	}
	if len(r.Repository) > 0 {
		var repo PluginRepository
		if err := json.Unmarshal(r.Repository, &repo); err == nil {
			resp.Repository = &repo
		}
	}
	if len(r.Icons) > 0 {
		var icons []PluginIcon
		if err := json.Unmarshal(r.Icons, &icons); err == nil {
			resp.Icons = icons
		}
	}
	if len(r.Metadata) > 0 {
		resp.Metadata = make(map[string]any)
		_ = json.Unmarshal(r.Metadata, &resp.Metadata)
	}
	if len(r.ExtensionMeta) > 0 {
		resp.Meta = make(map[string]any)
		_ = json.Unmarshal(r.ExtensionMeta, &resp.Meta)
	}
	return resp
}

// ListPluginsRowToPlugin maps a sqlc ListPluginsRow to a service Plugin.
func ListPluginsRowToPlugin(row sqlc.ListPluginsRow) *Plugin {
	return rowToPlugin(pluginRow{
		ID:              row.VersionID,
		Name:            row.Name,
		Version:         row.Version,
		IsLatest:        row.IsLatest,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
		Description:     row.Description,
		Title:           row.Title,
		PluginVersionID: row.VersionID,
		Namespace:       row.Namespace,
		Status:          row.Status,
		License:         row.License,
		Repository:      row.Repository,
		Icons:           row.Icons,
		Metadata:        row.Metadata,
		ExtensionMeta:   row.ExtensionMeta,
	})
}

// GetPluginVersionRowToPlugin maps a sqlc GetPluginVersionRow to a service Plugin.
func GetPluginVersionRowToPlugin(row sqlc.GetPluginVersionRow) *Plugin {
	return rowToPlugin(pluginRow{
		ID:              row.ID,
		Name:            row.Name,
		Version:         row.Version,
		IsLatest:        row.IsLatest,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
		Description:     row.Description,
		Title:           row.Title,
		PluginVersionID: row.PluginVersionID,
		Namespace:       row.Namespace,
		Status:          row.Status,
		License:         row.License,
		Repository:      row.Repository,
		Icons:           row.Icons,
		Metadata:        row.Metadata,
		ExtensionMeta:   row.ExtensionMeta,
	})
}
