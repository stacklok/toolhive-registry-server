// Package service defines skill-related types returned by the service layer
// and mapping from sqlc (database) row types.
package service

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
)

// SkillRepository represents source repository metadata at the service layer.
type SkillRepository struct {
	URL  string `json:"url,omitempty"`
	Type string `json:"type,omitempty"`
}

// SkillPackage represents a package for a skill at the service layer.
type SkillPackage struct {
	RegistryType string `json:"registryType"`
	Identifier   string `json:"identifier,omitempty"`
	Digest       string `json:"digest,omitempty"`
	MediaType    string `json:"mediaType,omitempty"`
	URL          string `json:"url,omitempty"`
	Ref          string `json:"ref,omitempty"`
	Commit       string `json:"commit,omitempty"`
	Subfolder    string `json:"subfolder,omitempty"`
}

// SkillIcon represents a display icon for a skill at the service layer.
type SkillIcon struct {
	Src   string `json:"src"`
	Size  string `json:"size,omitempty"`
	Type  string `json:"type,omitempty"`
	Label string `json:"label,omitempty"`
}

// Skill is a single skill returned by ListSkills, ListSkillVersions,
// GetSkillVersion, GetLatestSkillVersion, and PublishSkill.
type Skill struct {
	ID            string           `json:"id,omitempty"`
	Namespace     string           `json:"namespace"`
	Name          string           `json:"name"`
	Description   string           `json:"description"`
	Version       string           `json:"version"`
	Status        string           `json:"status,omitempty"`
	Title         string           `json:"title,omitempty"`
	License       string           `json:"license,omitempty"`
	Compatibility string           `json:"compatibility,omitempty"`
	AllowedTools  []string         `json:"allowedTools,omitempty"`
	Repository    *SkillRepository `json:"repository,omitempty"`
	Icons         []SkillIcon      `json:"icons,omitempty"`
	Packages      []SkillPackage   `json:"packages,omitempty"`
	Metadata      map[string]any   `json:"metadata,omitempty"`
	Meta          map[string]any   `json:"_meta,omitempty"`
	IsLatest      bool             `json:"isLatest,omitempty"`
	CreatedAt     time.Time        `json:"createdAt,omitempty"`
	UpdatedAt     time.Time        `json:"updatedAt,omitempty"`
}

// ListSkillsResult contains the result of a ListSkills operation with pagination.
type ListSkillsResult struct {
	Skills     []*Skill `json:"skills"`
	NextCursor string   `json:"-"`
}

// skillRow holds the common shape of sqlc skill list/get rows for mapping.
type skillRow struct {
	ID            uuid.UUID
	Name          string
	Version       string
	IsLatest      bool
	CreatedAt     *time.Time
	UpdatedAt     *time.Time
	Description   *string
	Title         *string
	SkillEntryID  uuid.UUID
	Namespace     string
	Status        sqlc.SkillStatus
	License       *string
	Compatibility *string
	AllowedTools  []string
	Repository    []byte
	Icons         []byte
	Metadata      []byte
	ExtensionMeta []byte
}

func rowToSkill(r skillRow) *Skill {
	resp := &Skill{
		ID:           r.ID.String(),
		Namespace:    r.Namespace,
		Name:         r.Name,
		Version:      r.Version,
		IsLatest:     r.IsLatest,
		Status:       string(r.Status),
		AllowedTools: r.AllowedTools,
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
	if r.Compatibility != nil {
		resp.Compatibility = *r.Compatibility
	}
	if r.CreatedAt != nil {
		resp.CreatedAt = *r.CreatedAt
	}
	if r.UpdatedAt != nil {
		resp.UpdatedAt = *r.UpdatedAt
	}
	if len(r.Repository) > 0 {
		var repo SkillRepository
		if err := json.Unmarshal(r.Repository, &repo); err == nil {
			resp.Repository = &repo
		}
	}
	if len(r.Icons) > 0 {
		var icons []SkillIcon
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

// ListSkillsRowToSkill maps a sqlc ListSkillsRow to a service Skill.
func ListSkillsRowToSkill(row sqlc.ListSkillsRow) *Skill {
	return rowToSkill(skillRow{
		ID:            row.EntryID,
		Name:          row.Name,
		Version:       row.Version,
		IsLatest:      row.IsLatest,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
		Description:   row.Description,
		Title:         row.Title,
		Namespace:     row.Namespace,
		Status:        row.Status,
		License:       row.License,
		Compatibility: row.Compatibility,
		AllowedTools:  row.AllowedTools,
		Repository:    row.Repository,
		Icons:         row.Icons,
		Metadata:      row.Metadata,
		ExtensionMeta: row.ExtensionMeta,
	})
}

// GetSkillVersionRowToSkill maps a sqlc GetSkillVersionRow to a service Skill.
func GetSkillVersionRowToSkill(row sqlc.GetSkillVersionRow) *Skill {
	return rowToSkill(skillRow{
		ID:            row.ID,
		Name:          row.Name,
		Version:       row.Version,
		IsLatest:      row.IsLatest,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
		Description:   row.Description,
		Title:         row.Title,
		SkillEntryID:  row.SkillEntryID,
		Namespace:     row.Namespace,
		Status:        row.Status,
		License:       row.License,
		Compatibility: row.Compatibility,
		AllowedTools:  row.AllowedTools,
		Repository:    row.Repository,
		Icons:         row.Icons,
		Metadata:      row.Metadata,
		ExtensionMeta: row.ExtensionMeta,
	})
}

// PublishSkillOptions is the options for the PublishSkill operation
type PublishSkillOptions struct {
	RegistryName string
}

// ListSkillsOptions is the options for the ListSkills and ListSkillVersions
// operations.
type ListSkillsOptions struct {
	RegistryName string
	Namespace    string
	Name         *string
	Version      *string
	Search       *string
	Limit        int
	Cursor       *string
}

// GetSkillVersionOptions is the options for the GetSkillVersion operation.
type GetSkillVersionOptions struct {
	RegistryName string
	Namespace    string
	Name         string
	Version      string
}

// DeleteSkillVersionOptions is the options for the DeleteSkillVersion operation
type DeleteSkillVersionOptions struct {
	RegistryName string
	Namespace    string
	Name         string
	Version      string
}
