// Package service provides the business logic for the MCP registry API
package service

import (
	"context"
	"errors"
	"time"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"

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
	// ErrVersionAlreadyExists is returned when attempting to publish a version that already exists
	ErrVersionAlreadyExists = errors.New("version already exists")
	// ErrConfigSource is returned when attempting to modify a CONFIG-created source via API
	ErrConfigSource = errors.New("cannot modify config-created source via API")
	// ErrInvalidSourceConfig is returned when source configuration is invalid
	ErrInvalidSourceConfig = errors.New("invalid source configuration")
	// ErrSourceAlreadyExists is returned when attempting to create a source that already exists
	ErrSourceAlreadyExists = errors.New("source already exists")
	// ErrSourceTypeChangeNotAllowed is returned when attempting to change a source's type
	ErrSourceTypeChangeNotAllowed = errors.New("changing source type is not allowed")
	// ErrSourceNotFound is returned when a source is not found
	ErrSourceNotFound = errors.New("source not found")
	// ErrNoManagedSource is returned when no managed source is found
	ErrNoManagedSource = errors.New("no managed source found")
	// ErrRegistryAlreadyExists is returned when attempting to create a registry that already exists
	ErrRegistryAlreadyExists = errors.New("registry already exists")
	// ErrConfigRegistry is returned when attempting to modify a CONFIG-created registry via API
	ErrConfigRegistry = errors.New("cannot modify config-created registry via API")
	// ErrInvalidRegistryConfig is returned when registry configuration is invalid
	ErrInvalidRegistryConfig = errors.New("invalid registry configuration")
	// ErrSourceInUse is returned when attempting to delete a source that is linked to registries
	ErrSourceInUse = errors.New("source is referenced by one or more registries")
	// ErrClaimsMismatch is returned when publish claims do not match the existing entry's claims
	ErrClaimsMismatch = errors.New("claims mismatch")
	// ErrClaimsInsufficient is returned when the caller's JWT claims do not cover a resource's claims
	ErrClaimsInsufficient = errors.New("insufficient claims")
)

//go:generate mockgen -destination=mocks/mock_service.go -package=mocks -source=service.go Service

// RegistryService defines the interface for registry operations
type RegistryService interface {

	// ********** COMMON OPERATIONS **********

	// CheckReadiness checks if the regSvc is ready to serve requests
	CheckReadiness(ctx context.Context) error

	// ********** MCP OPERATIONS **********

	// ListServers returns all servers in the registry with pagination info
	ListServers(ctx context.Context, opts ...Option) (*ListServersResult, error)

	// ListServerVersions returns all versions of a specific server
	ListServerVersions(ctx context.Context, opts ...Option) ([]*upstreamv0.ServerJSON, error)

	// GetServerVersion returns a specific server version by name
	GetServerVersion(ctx context.Context, opts ...Option) (*upstreamv0.ServerJSON, error)

	// PublishServerVersion publishes a server version to a managed registry
	PublishServerVersion(ctx context.Context, opts ...Option) (*upstreamv0.ServerJSON, error)

	// DeleteServerVersion removes a server version from a managed registry
	DeleteServerVersion(ctx context.Context, opts ...Option) error

	// ********** SOURCE OPERATIONS **********

	// ListSources returns all configured sources
	ListSources(ctx context.Context) ([]SourceInfo, error)

	// GetSourceByName returns a single source by name
	GetSourceByName(ctx context.Context, name string) (*SourceInfo, error)

	// CreateSource creates a new API-managed source
	CreateSource(ctx context.Context, name string, req *SourceCreateRequest) (*SourceInfo, error)

	// UpdateSource updates an existing API-managed source
	UpdateSource(ctx context.Context, name string, req *SourceCreateRequest) (*SourceInfo, error)

	// DeleteSource deletes an API-managed source
	DeleteSource(ctx context.Context, name string) error

	// ListSourceEntries returns all entries for a source (unshadowed, all types)
	ListSourceEntries(ctx context.Context, sourceName string) ([]SourceEntryInfo, error)

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

	// ListRegistryEntries returns all entries across a registry's linked sources (unshadowed, lightweight)
	ListRegistryEntries(ctx context.Context, registryName string) ([]RegistryEntryInfo, error)

	// ProcessInlineSourceData processes inline data for a managed/file registry
	ProcessInlineSourceData(ctx context.Context, name string, data string, format string) error

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

// SourceInfo represents detailed information about a source
type SourceInfo struct {
	Name         string               `json:"name"`
	Type         string               `json:"type"`                   // MANAGED, FILE, REMOTE, KUBERNETES
	CreationType CreationType         `json:"creationType,omitempty"` // API or CONFIG
	SourceType   config.SourceType    `json:"sourceType,omitempty"`   // git, api, file, managed, kubernetes
	Format       string               `json:"format,omitempty"`       // toolhive or upstream
	SourceConfig any                  `json:"sourceConfig,omitempty"` // Type-specific source configuration
	FilterConfig *config.FilterConfig `json:"filterConfig,omitempty"` // Filtering rules
	SyncSchedule string               `json:"syncSchedule,omitempty"` // Sync interval string
	Claims       map[string]any       `json:"claims,omitempty"`       // Authorization claims
	SyncStatus   *SourceSyncStatus    `json:"syncStatus,omitempty"`
	CreatedAt    time.Time            `json:"createdAt"`
	UpdatedAt    time.Time            `json:"updatedAt"`
}

// RegistryInfo represents detailed information about a registry
type RegistryInfo struct {
	Name         string         `json:"name"`
	Claims       map[string]any `json:"claims,omitempty"`
	CreationType CreationType   `json:"creationType,omitempty"`
	Sources      []string       `json:"sources"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
}

// SourceSyncStatus represents the sync status of a registry
type SourceSyncStatus struct {
	Phase        string     `json:"phase"`                  // complete, syncing, failed
	LastSyncTime *time.Time `json:"lastSyncTime,omitempty"` // Last successful sync
	LastAttempt  *time.Time `json:"lastAttempt,omitempty"`  // Last sync attempt
	AttemptCount int        `json:"attemptCount"`           // Number of sync attempts
	ServerCount  int        `json:"serverCount"`            // Number of servers in registry
	Message      string     `json:"message,omitempty"`      // Status or error message
}

// SourceListResponse represents the response for listing sources
type SourceListResponse struct {
	Sources []SourceInfo `json:"sources"`
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

// SourceEntryInfo represents a single entry within a source, including all versions.
type SourceEntryInfo struct {
	EntryType string             `json:"entryType"`
	Name      string             `json:"name"`
	Claims    map[string]any     `json:"claims,omitempty"`
	Versions  []EntryVersionInfo `json:"versions"`
}

// EntryVersionInfo represents a single version of an entry.
type EntryVersionInfo struct {
	Version     string    `json:"version"`
	Title       string    `json:"title,omitempty"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// SourceEntriesResponse is the JSON envelope for listing source entries.
type SourceEntriesResponse struct {
	Entries []SourceEntryInfo `json:"entries"`
}

// RegistryEntryInfo represents a lightweight entry in a registry listing.
type RegistryEntryInfo struct {
	EntryType  string `json:"entryType"`
	Name       string `json:"name"`
	Version    string `json:"version"`
	SourceName string `json:"sourceName"`
}

// RegistryEntriesResponse is the JSON envelope for listing registry entries.
type RegistryEntriesResponse struct {
	Entries []RegistryEntryInfo `json:"entries"`
}
