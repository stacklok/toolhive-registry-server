// Package service provides the business logic for the MCP registry API
package service

import (
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// RegistrySourceType represents the type of registry data source
type RegistrySourceType string

const (
	// SourceTypeGit indicates a git repository source
	SourceTypeGit RegistrySourceType = "git"
	// SourceTypeAPI indicates an API endpoint source
	SourceTypeAPI RegistrySourceType = "api"
	// SourceTypeFile indicates a local file or URL source
	SourceTypeFile RegistrySourceType = "file"
	// SourceTypeManaged indicates a managed registry (no sync)
	SourceTypeManaged RegistrySourceType = "managed"
	// SourceTypeKubernetes indicates a Kubernetes discovery source (no sync)
	SourceTypeKubernetes RegistrySourceType = "kubernetes"
)

// CreationType indicates how the registry was created
type CreationType string

const (
	// CreationTypeAPI indicates the registry was created via API
	CreationTypeAPI CreationType = "API"
	// CreationTypeCONFIG indicates the registry was created from config file
	CreationTypeCONFIG CreationType = "CONFIG"
)

// RegistryCreateRequest represents the request body for PUT /registries/{name}
type RegistryCreateRequest struct {
	Format     string                   `json:"format,omitempty"`     // "toolhive" or "upstream"
	Git        *config.GitConfig        `json:"git,omitempty"`        // Git repository source
	API        *config.APIConfig        `json:"api,omitempty"`        // API endpoint source
	File       *config.FileConfig       `json:"file,omitempty"`       // Local file or URL source
	Managed    *config.ManagedConfig    `json:"managed,omitempty"`    // Managed registry (no sync)
	Kubernetes *config.KubernetesConfig `json:"kubernetes,omitempty"` // Kubernetes discovery source
	SyncPolicy *config.SyncPolicyConfig `json:"syncPolicy,omitempty"` // Sync schedule configuration
	Filter     *config.FilterConfig     `json:"filter,omitempty"`     // Name/tag filtering rules
}

// GetSourceType returns the source type based on which config is set
func (r *RegistryCreateRequest) GetSourceType() RegistrySourceType {
	switch {
	case r.Git != nil:
		return SourceTypeGit
	case r.API != nil:
		return SourceTypeAPI
	case r.File != nil:
		return SourceTypeFile
	case r.Managed != nil:
		return SourceTypeManaged
	case r.Kubernetes != nil:
		return SourceTypeKubernetes
	default:
		return ""
	}
}

// CountSourceTypes returns the number of source types configured
func (r *RegistryCreateRequest) CountSourceTypes() int {
	count := 0
	if r.Git != nil {
		count++
	}
	if r.API != nil {
		count++
	}
	if r.File != nil {
		count++
	}
	if r.Managed != nil {
		count++
	}
	if r.Kubernetes != nil {
		count++
	}
	return count
}

// IsNonSyncedType returns true if the source type doesn't require syncing
func (r *RegistryCreateRequest) IsNonSyncedType() bool {
	sourceType := r.GetSourceType()
	// Managed and Kubernetes don't sync
	if sourceType == SourceTypeManaged || sourceType == SourceTypeKubernetes {
		return true
	}
	// File with inline data doesn't sync (processed immediately)
	if r.IsInlineData() {
		return true
	}
	return false
}

// IsInlineData returns true if this is a file source with inline data
func (r *RegistryCreateRequest) IsInlineData() bool {
	return r.File != nil && r.File.Data != ""
}

// GetSourceConfig returns the active source configuration
func (r *RegistryCreateRequest) GetSourceConfig() interface{} {
	switch {
	case r.Git != nil:
		return r.Git
	case r.API != nil:
		return r.API
	case r.File != nil:
		return r.File
	case r.Managed != nil:
		return r.Managed
	case r.Kubernetes != nil:
		return r.Kubernetes
	default:
		return nil
	}
}
