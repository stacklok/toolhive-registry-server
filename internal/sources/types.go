package sources

import (
	"context"

	toolhivetypes "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/formats"
)

// RegistryDataValidator is an interface for validating registry source configurations
type RegistryDataValidator interface {
	// ValidateData validates raw data and returns a parsed UpstreamRegistry
	ValidateData(data []byte) (*toolhivetypes.UpstreamRegistry, error)
}

//go:generate mockgen -destination=mocks/mock_registry_handler.go -package=mocks -source=types.go RegistryHandler,RegistryHandlerFactory

// RegistryHandler is an interface with methods to fetch data from external data sources
type RegistryHandler interface {
	// FetchRegistry retrieves data from the source and returns the result
	FetchRegistry(ctx context.Context, regCfg *config.SourceConfig) (*FetchResult, error)

	// Validate validates the registry configuration
	Validate(regCfg *config.SourceConfig) error
}

// FetchResult contains the result of a fetch operation
type FetchResult struct {
	// Registry is the parsed registry data in unified UpstreamRegistry format
	Registry *toolhivetypes.UpstreamRegistry

	// Hash is the SHA256 hash of the serialized data for change detection
	Hash string

	// ServerCount is the number of servers found in the registry data
	ServerCount int

	// SkillCount is the number of skills found in the registry data
	SkillCount int

	// PluginCount is the number of plugins found in the registry data
	PluginCount int
}

// NewFetchResult creates a new FetchResult from a UpstreamRegistry instance and pre-calculated hash
// The hash should be calculated by the registry handler from the raw source data
func NewFetchResult(reg *toolhivetypes.UpstreamRegistry, hash string) *FetchResult {
	serverCount := 0
	skillCount := 0
	pluginCount := 0
	if reg != nil {
		serverCount = len(reg.Data.Servers)
		skillCount = len(reg.Data.Skills)
		pluginCount = len(reg.Data.Plugins)
	}

	return &FetchResult{
		Registry:    reg,
		Hash:        hash,
		ServerCount: serverCount,
		SkillCount:  skillCount,
		PluginCount: pluginCount,
	}
}

// RegistryHandlerFactory creates registry handlers based on registry configuration
type RegistryHandlerFactory interface {
	// CreateHandler creates a registry handler for the given registry configuration
	// The source type is inferred from which field is present (Git/API/File)
	CreateHandler(regCfg *config.SourceConfig) (RegistryHandler, error)
}

// defaultRegistryDataValidator is the default implementation of RegistryDataValidator
type defaultRegistryDataValidator struct{}

var _ RegistryDataValidator = (*defaultRegistryDataValidator)(nil)

// NewRegistryDataValidator creates a new default registry data validator
func NewRegistryDataValidator() RegistryDataValidator {
	return &defaultRegistryDataValidator{}
}

// ValidateData validates raw data and returns a parsed UpstreamRegistry
func (*defaultRegistryDataValidator) ValidateData(data []byte) (*toolhivetypes.UpstreamRegistry, error) {
	return formats.DecodeUpstream(data)
}
