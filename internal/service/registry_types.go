// Package service provides the business logic for the MCP registry API
package service

import (
	"github.com/stacklok/toolhive-registry-server/internal/config"
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
func (r *RegistryCreateRequest) GetSourceType() config.SourceType {
	switch {
	case r.Git != nil:
		return config.SourceTypeGit
	case r.API != nil:
		return config.SourceTypeAPI
	case r.File != nil:
		return config.SourceTypeFile
	case r.Managed != nil:
		return config.SourceTypeManaged
	case r.Kubernetes != nil:
		return config.SourceTypeKubernetes
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
	if sourceType == config.SourceTypeManaged || sourceType == config.SourceTypeKubernetes {
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
func (r *RegistryCreateRequest) GetSourceConfig() any {
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

// DeployedServer represents a deployed MCP server in Kubernetes
type DeployedServer struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Status      string `json:"status"`
	Image       string `json:"image"`
	Transport   string `json:"transport"`
	Ready       bool   `json:"ready"`
	EndpointURL string `json:"endpoint_url,omitempty"`
}
