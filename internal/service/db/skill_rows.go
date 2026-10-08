package database

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// skillRow holds the common shape of sqlc skill list/get rows for mapping.
type skillModel = service.Skill
type skillRepository = service.SkillRepository
type skillIcon = service.SkillIcon

type skillRow struct {
	ID             uuid.UUID
	Name           string
	Version        string
	IsLatest       bool
	CreatedAt      *time.Time
	UpdatedAt      *time.Time
	Description    *string
	Title          *string
	SkillVersionID uuid.UUID
	Namespace      string
	Status         sqlc.SkillStatus
	License        *string
	Compatibility  *string
	AllowedTools   []string
	Repository     []byte
	Icons          []byte
	Metadata       []byte
	ExtensionMeta  []byte
}

func rowToSkill(r skillRow) *skillModel {
	resp := &skillModel{
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
		var repo skillRepository
		if err := json.Unmarshal(r.Repository, &repo); err == nil {
			resp.Repository = &repo
		}
	}
	if len(r.Icons) > 0 {
		var icons []skillIcon
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

// listSkillsRowToSkill maps a sqlc ListSkillsRow to a service Skill.
func listSkillsRowToSkill(row sqlc.ListSkillsRow) *skillModel {
	return rowToSkill(skillRow{
		ID:            row.VersionID,
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

// getSkillVersionRowToSkill maps a sqlc GetSkillVersionRow to a service Skill.
func getSkillVersionRowToSkill(row sqlc.GetSkillVersionRow) *skillModel {
	return rowToSkill(skillRow{
		ID:             row.ID,
		Name:           row.Name,
		Version:        row.Version,
		IsLatest:       row.IsLatest,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
		Description:    row.Description,
		Title:          row.Title,
		SkillVersionID: row.SkillVersionID,
		Namespace:      row.Namespace,
		Status:         row.Status,
		License:        row.License,
		Compatibility:  row.Compatibility,
		AllowedTools:   row.AllowedTools,
		Repository:     row.Repository,
		Icons:          row.Icons,
		Metadata:       row.Metadata,
		ExtensionMeta:  row.ExtensionMeta,
	})
}
