// Package service provides the business logic for the MCP registry API
package service

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
	Format     string               `json:"format,omitempty"`     // "toolhive" or "upstream"
	Git        *GitSourceConfig     `json:"git,omitempty"`        // Git repository source
	API        *APISourceConfig     `json:"api,omitempty"`        // API endpoint source
	File       *FileSourceConfig    `json:"file,omitempty"`       // Local file or URL source
	Managed    *ManagedSourceConfig `json:"managed,omitempty"`    // Managed registry (no sync)
	Kubernetes *KubernetesConfig    `json:"kubernetes,omitempty"` // Kubernetes discovery source
	SyncPolicy *SyncPolicyConfig    `json:"syncPolicy,omitempty"` // Sync schedule configuration
	Filter     *FilterConfig        `json:"filter,omitempty"`     // Name/tag filtering rules
}

// GitSourceConfig defines Git source settings (no credentials stored)
type GitSourceConfig struct {
	Repository string `json:"repository"`       // Git repository URL (required)
	Branch     string `json:"branch,omitempty"` // Branch to sync from (mutually exclusive with Tag/Commit)
	Tag        string `json:"tag,omitempty"`    // Tag to sync from
	Commit     string `json:"commit,omitempty"` // Commit hash to sync from
	Path       string `json:"path,omitempty"`   // Path to registry file within repository
}

// APISourceConfig defines API source settings
type APISourceConfig struct {
	Endpoint string `json:"endpoint"` // Base API URL (required)
}

// FileSourceConfig defines file source settings
type FileSourceConfig struct {
	Path    string `json:"path,omitempty"`    // Local file path (mutually exclusive with URL)
	URL     string `json:"url,omitempty"`     // HTTP/HTTPS URL
	Timeout string `json:"timeout,omitempty"` // Request timeout (e.g., "30s")
}

// ManagedSourceConfig defines managed registry settings
type ManagedSourceConfig struct {
	// Currently empty, placeholder for future configuration
}

// KubernetesConfig defines Kubernetes source settings
type KubernetesConfig struct {
	// Currently empty, placeholder for future configuration
}

// SyncPolicyConfig defines sync schedule
type SyncPolicyConfig struct {
	Interval string `json:"interval"` // Sync interval (e.g., "30m", "1h")
}

// FilterConfig defines filtering rules for registry servers
type FilterConfig struct {
	Names *NameFilterConfig `json:"names,omitempty"` // Name-based filtering
	Tags  *TagFilterConfig  `json:"tags,omitempty"`  // Tag-based filtering
}

// NameFilterConfig defines name-based filtering rules
type NameFilterConfig struct {
	Include []string `json:"include,omitempty"` // Patterns to include
	Exclude []string `json:"exclude,omitempty"` // Patterns to exclude
}

// TagFilterConfig defines tag-based filtering rules
type TagFilterConfig struct {
	Include []string `json:"include,omitempty"` // Tags to include
	Exclude []string `json:"exclude,omitempty"` // Tags to exclude
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
	return sourceType == SourceTypeManaged || sourceType == SourceTypeKubernetes
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
