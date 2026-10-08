package database

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// pluginRow holds the common shape of sqlc plugin list/get rows for mapping.
type pluginModel = service.Plugin
type pluginRepository = service.PluginRepository
type pluginIcon = service.PluginIcon

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

func rowToPlugin(r pluginRow) *pluginModel {
	resp := &pluginModel{
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
		var repo pluginRepository
		if err := json.Unmarshal(r.Repository, &repo); err == nil {
			resp.Repository = &repo
		}
	}
	if len(r.Icons) > 0 {
		var icons []pluginIcon
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

// listPluginsRowToPlugin maps a sqlc ListPluginsRow to a service Plugin.
func listPluginsRowToPlugin(row sqlc.ListPluginsRow) *pluginModel {
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

// getPluginVersionRowToPlugin maps a sqlc GetPluginVersionRow to a service Plugin.
func getPluginVersionRowToPlugin(row sqlc.GetPluginVersionRow) *pluginModel {
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
