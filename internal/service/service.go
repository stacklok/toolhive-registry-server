// Package service provides the business logic for the MCP registry API
package service

import (
	"context"
	"errors"
	"time"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// Pagination constants used by all RegistryService implementations.
// These values are aligned with the upstream MCP Registry API specification.
const (
	// DefaultPageSize is the default number of items per page when no limit is specified.
	// This matches the upstream MCP Registry API default of 30.
	DefaultPageSize = 30
	// MaxPageSize is the maximum allowed items per page to prevent potential DoS.
	MaxPageSize = 1000

	// SkillPackageTypeOCI is the type for OCI packages
	SkillPackageTypeOCI = "oci"
	// SkillPackageTypeGit is the type for Git packages
	SkillPackageTypeGit = "git"
)

var (
	// ErrNotFound is returned when a server is not found
	ErrNotFound = errors.New("not found")
	// ErrNotImplemented is returned when a feature is not implemented
	ErrNotImplemented = errors.New("not implemented")
	// ErrRegistryNotFound is returned when a registry is not found
	ErrRegistryNotFound = errors.New("registry not found")
	// ErrNotManagedRegistry is returned when attempting write operations on a non-managed registry
	ErrNotManagedRegistry = errors.New("registry is not managed")
	// ErrVersionAlreadyExists is returned when attempting to publish a version that already exists
	ErrVersionAlreadyExists = errors.New("version already exists")
	// ErrConfigRegistry is returned when attempting to modify a CONFIG-created registry via API
	ErrConfigRegistry = errors.New("cannot modify config-created registry via API")
	// ErrInvalidRegistryConfig is returned when registry configuration is invalid
	ErrInvalidRegistryConfig = errors.New("invalid registry configuration")
	// ErrRegistryAlreadyExists is returned when attempting to create a registry that already exists
	ErrRegistryAlreadyExists = errors.New("registry already exists")
	// ErrSourceTypeChangeNotAllowed is returned when attempting to change a registry's source type
	ErrSourceTypeChangeNotAllowed = errors.New("changing registry source type is not allowed")
)

//go:generate mockgen -destination=mocks/mock_service.go -package=mocks -source=service.go Service

// RegistryService defines the interface for registry operations
type RegistryService interface {

	// ********** COMMON OPERATIONS **********

	// CheckReadiness checks if the regSvc is ready to serve requests
	CheckReadiness(ctx context.Context) error

	// GetRegistry returns the registry data with metadata
	GetRegistry(ctx context.Context) (*toolhivetypes.UpstreamRegistry, string, error) // returns registry, source, error

	// ********** MCP OPERATIONS **********

	// ListServers returns all servers in the registry with pagination info
	ListServers(ctx context.Context, opts ...Option) (*ListServersResult, error)

	// ListServerVersions returns all versions of a specific server
	ListServerVersions(ctx context.Context, opts ...Option) ([]*upstreamv0.ServerJSON, error)

	// GetServer returns a specific server by name
	GetServerVersion(ctx context.Context, opts ...Option) (*upstreamv0.ServerJSON, error)

	// PublishServerVersion publishes a server version to a managed registry
	PublishServerVersion(ctx context.Context, opts ...Option) (*upstreamv0.ServerJSON, error)

	// DeleteServerVersion removes a server version from a managed registry
	DeleteServerVersion(ctx context.Context, opts ...Option) error

	// ********** REGISTRY OPERATIONS **********

	// ListRegistries returns all configured registries
	ListRegistries(ctx context.Context) ([]RegistryInfo, error)

	// GetRegistryByName returns a single registry by name
	GetRegistryByName(ctx context.Context, name string) (*RegistryInfo, error)

	// CreateRegistry creates a new API-managed registry
	CreateRegistry(ctx context.Context, name string, req *RegistryCreateRequest) (*RegistryInfo, error)

	// UpdateRegistry updates an existing API-managed registry
	UpdateRegistry(ctx context.Context, name string, req *RegistryCreateRequest) (*RegistryInfo, error)

	// DeleteRegistry deletes an API-managed registry
	DeleteRegistry(ctx context.Context, name string) error

	// ProcessInlineRegistryData processes inline data for a managed/file registry
	ProcessInlineRegistryData(ctx context.Context, name string, data string, format string) error

	// ********** SKILL OPERATIONS **********

	// ListSkills lists skills in a registry with cursor-based pagination
	ListSkills(ctx context.Context, opts ...Option) (*ListSkillsResult, error)

	// GetSkillVersion gets a specific skill version. If the version is
	// "latest", the latest version will be returned.
	GetSkillVersion(ctx context.Context, opts ...Option) (*Skill, error)

	// PublishSkill publishes a skill
	PublishSkill(ctx context.Context, skill *Skill, opts ...Option) (*Skill, error)

	// DeleteSkillVersion deletes a skill version
	DeleteSkillVersion(ctx context.Context, opts ...Option) error
}

// RegistryInfo represents detailed information about a registry
type RegistryInfo struct {
	Name         string               `json:"name"`
	Type         string               `json:"type"`                   // MANAGED, FILE, REMOTE, KUBERNETES
	CreationType CreationType         `json:"creationType,omitempty"` // API or CONFIG
	SourceType   config.SourceType    `json:"sourceType,omitempty"`   // git, api, file, managed, kubernetes
	Format       string               `json:"format,omitempty"`       // toolhive or upstream
	SourceConfig any                  `json:"sourceConfig,omitempty"` // Type-specific source configuration
	FilterConfig *config.FilterConfig `json:"filterConfig,omitempty"` // Filtering rules
	SyncSchedule string               `json:"syncSchedule,omitempty"` // Sync interval string
	SyncStatus   *RegistrySyncStatus  `json:"syncStatus,omitempty"`
	CreatedAt    time.Time            `json:"createdAt"`
	UpdatedAt    time.Time            `json:"updatedAt"`
}

// RegistrySyncStatus represents the sync status of a registry
type RegistrySyncStatus struct {
	Phase        string     `json:"phase"`                  // complete, syncing, failed
	LastSyncTime *time.Time `json:"lastSyncTime,omitempty"` // Last successful sync
	LastAttempt  *time.Time `json:"lastAttempt,omitempty"`  // Last sync attempt
	AttemptCount int        `json:"attemptCount"`           // Number of sync attempts
	ServerCount  int        `json:"serverCount"`            // Number of servers in registry
	Message      string     `json:"message,omitempty"`      // Status or error message
}

// RegistryListResponse represents the response for listing registries
type RegistryListResponse struct {
	Registries []RegistryInfo `json:"registries"`
}

// ListServersResult contains the result of a ListServers operation with pagination info.
// It wraps the server list with cursor-based pagination metadata.
type ListServersResult struct {
	// Servers is the list of servers matching the query
	Servers []*upstreamv0.ServerJSON
	// NextCursor is the cursor to use for fetching the next page of results.
	// Empty string indicates no more results are available.
	NextCursor string
}
