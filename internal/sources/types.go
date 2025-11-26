package sources

import (
	"context"
	"encoding/json"
	"fmt"

	toolhiveregistry "github.com/stacklok/toolhive/pkg/registry"
	"github.com/stacklok/toolhive/pkg/registry/converters"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// RegistryDataValidator is an interface for validating registry source configurations
type RegistryDataValidator interface {
	// ValidateData validates raw data and returns a parsed UpstreamRegistry
	ValidateData(data []byte, format string) (*toolhivetypes.UpstreamRegistry, error)
}

//go:generate mockgen -destination=mocks/mock_registry_handler.go -package=mocks -source=types.go RegistryHandler,RegistryHandlerFactory

// RegistryHandler is an interface with methods to fetch data from external data sources
type RegistryHandler interface {
	// FetchRegistry retrieves data from the source and returns the result
	FetchRegistry(ctx context.Context, regCfg *config.RegistryConfig) (*FetchResult, error)

	// Validate validates the registry configuration
	Validate(regCfg *config.RegistryConfig) error

	// CurrentHash returns the current hash of the source data without performing a full fetch
	CurrentHash(ctx context.Context, regCfg *config.RegistryConfig) (string, error)
}

// FetchResult contains the result of a fetch operation
type FetchResult struct {
	// Registry is the parsed registry data in unified UpstreamRegistry format
	Registry *toolhivetypes.UpstreamRegistry

	// Hash is the SHA256 hash of the serialized data for change detection
	Hash string

	// ServerCount is the number of servers found in the registry data
	ServerCount int

	// Format indicates the original format of the source data
	Format string
}

// NewFetchResult creates a new FetchResult from a UpstreamRegistry instance and pre-calculated hash
// The hash should be calculated by the registry handler to ensure consistency with CurrentHash
func NewFetchResult(reg *toolhivetypes.UpstreamRegistry, hash string, format string) *FetchResult {
	serverCount := 0
	if reg != nil {
		serverCount = len(reg.Data.Servers)
	}

	return &FetchResult{
		Registry:    reg,
		Hash:        hash,
		ServerCount: serverCount,
		Format:      format,
	}
}

// RegistryHandlerFactory creates registry handlers based on registry configuration
type RegistryHandlerFactory interface {
	// CreateHandler creates a registry handler for the given registry configuration
	// The source type is inferred from which field is present (Git/API/File)
	CreateHandler(regCfg *config.RegistryConfig) (RegistryHandler, error)
}

// defaultRegistryDataValidator is the default implementation of RegistryDataValidator
type defaultRegistryDataValidator struct{}

var _ RegistryDataValidator = (*defaultRegistryDataValidator)(nil)

// NewRegistryDataValidator creates a new default registry data validator
func NewRegistryDataValidator() RegistryDataValidator {
	return &defaultRegistryDataValidator{}
}

// ValidateData validates raw data and returns a parsed UpstreamRegistry
func (*defaultRegistryDataValidator) ValidateData(data []byte, format string) (*toolhivetypes.UpstreamRegistry, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}

	switch format {
	case config.SourceFormatToolHive:
		return validateToolhiveFormatAndParse(data)
	case config.SourceFormatUpstream:
		return validateUpstreamFormatAndParse(data)
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
}

// validateToolhiveFormatAndParse validates data against ToolHive registry format and returns parsed UpstreamRegistry
func validateToolhiveFormatAndParse(data []byte) (*toolhivetypes.UpstreamRegistry, error) {
	// Use the existing schema validation from toolhive package
	if err := toolhiveregistry.ValidateRegistrySchema(data); err != nil {
		return nil, err
	}

	// Parse the validated data as ToolHive Registry
	var toolhiveReg toolhivetypes.Registry
	if err := json.Unmarshal(data, &toolhiveReg); err != nil {
		return nil, fmt.Errorf("failed to parse ToolHive registry format: %w", err)
	}

	// Convert to UpstreamRegistry using constructor
	serverReg, err := converters.NewUpstreamRegistryFromToolhiveRegistry(&toolhiveReg)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to UpstreamRegistry: %w", err)
	}

	return serverReg, nil
}

// validateUpstreamFormatAndParse validates data against upstream registry format and returns UpstreamRegistry
func validateUpstreamFormatAndParse(data []byte) (*toolhivetypes.UpstreamRegistry, error) {
	// Validate using toolhive's upstream registry schema validator
	if err := toolhiveregistry.ValidateUpstreamRegistry(data); err != nil {
		return nil, err
	}

	// Parse directly as UpstreamRegistry structure
	// This format has: { version, meta: { last_updated }, data: { servers: [...] } }
	var upstreamReg toolhivetypes.UpstreamRegistry
	if err := json.Unmarshal(data, &upstreamReg); err != nil {
		return nil, fmt.Errorf("failed to parse upstream registry format: %w", err)
	}

	// Validate we have at least one server
	if len(upstreamReg.Data.Servers) == 0 {
		return nil, fmt.Errorf("upstream registry must contain at least one server")
	}

	// Validate required fields for each server
	for i, server := range upstreamReg.Data.Servers {
		if server.Name == "" {
			return nil, fmt.Errorf("server at index %d: name is required", i)
		}
		if server.Description == "" {
			return nil, fmt.Errorf("server at index %d (%s): description is required", i, server.Name)
		}
	}

	return &upstreamReg, nil
}
