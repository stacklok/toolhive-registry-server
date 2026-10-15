// Package model defines registry entry versions independently of storage and identity.
// ServerJSON uses the upstream MCP schema; Skill and Plugin retain the registry
// service's version metadata in addition to their ToolHive payload fields.
package model

import (
	"time"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	thvregistry "github.com/stacklok/toolhive-core/registry/types"
)

// Server is the upstream MCP Registry server payload, not a competing schema.
type Server = upstreamv0.ServerJSON

// PackageFormatOCI and PackageFormatGit are the supported values for
// SkillPackage.RegistryType and PluginPackage.RegistryType.
const (
	PackageFormatOCI = "oci"
	PackageFormatGit = "git"
)

// SkillRepository describes a skill's source repository.
type SkillRepository struct {
	URL  string `json:"url,omitempty"`
	Type string `json:"type,omitempty"`
}

// SkillPackage describes a skill's package.
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

// SkillIcon describes a skill's display icon.
type SkillIcon struct {
	Src   string `json:"src"`
	Size  string `json:"size,omitempty"`
	Type  string `json:"type,omitempty"`
	Label string `json:"label,omitempty"`
}

// Skill is a catalog skill version, including storage-facing version metadata.
// Formats.SkillPayload converts it to the ToolHive consumer schema without
// exposing the catalog's ID, timestamps, or latest flag.
type Skill struct {
	ID            string                  `json:"id,omitempty"`
	Namespace     string                  `json:"namespace"`
	Name          string                  `json:"name"`
	Description   string                  `json:"description"`
	Version       string                  `json:"version"`
	Status        string                  `json:"status,omitempty"`
	Title         string                  `json:"title,omitempty"`
	License       string                  `json:"license,omitempty"`
	Compatibility string                  `json:"compatibility,omitempty"`
	AllowedTools  []string                `json:"allowedTools,omitempty"`
	Repository    *SkillRepository        `json:"repository,omitempty"`
	Icons         []SkillIcon             `json:"icons,omitempty"`
	Packages      []SkillPackage          `json:"packages,omitempty"`
	Metadata      map[string]any          `json:"metadata,omitempty"`
	Provenance    *thvregistry.Provenance `json:"provenance,omitempty"`
	Meta          map[string]any          `json:"_meta,omitempty"`
	IsLatest      bool                    `json:"isLatest,omitempty"`
	CreatedAt     time.Time               `json:"createdAt,omitempty"`
	UpdatedAt     time.Time               `json:"updatedAt,omitempty"`
}

// PluginRepository describes a plugin's source repository.
type PluginRepository = SkillRepository

// PluginPackage describes a plugin's package.
type PluginPackage = SkillPackage

// PluginIcon describes a plugin's display icon.
type PluginIcon = SkillIcon

// Plugin is a catalog plugin version, including storage-facing version metadata.
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

// Snapshot is the existing ToolHive upstream registry data format (all three kinds).
type Snapshot = thvregistry.UpstreamRegistry
