package sources

import (
	"context"
	"encoding/json"
	"fmt"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	toolhiveregistry "github.com/stacklok/toolhive/pkg/registry"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/types"

	"github.com/stacklok/toolhive-registry-server/pkg/config"
	"github.com/stacklok/toolhive-registry-server/pkg/registry"
)

// SourceDataValidator is an interface for validating registry source configurations
type SourceDataValidator interface {
	// ValidateData validates raw data and returns a parsed ServerRegistry
	ValidateData(data []byte, format string) (*registry.ServerRegistry, error)
}

//go:generate mockgen -destination=mocks/mock_source_handler.go -package=mocks -source=types.go SourceHandler,SourceHandlerFactory

// SourceHandler is an interface with methods to fetch data from external data sources
type SourceHandler interface {
	// FetchRegistry retrieves data from the source and returns the result
	FetchRegistry(ctx context.Context, cfg *config.Config) (*FetchResult, error)

	// Validate validates the source configuration
	Validate(source *config.SourceConfig) error

	// CurrentHash returns the current hash of the source data without performing a full fetch
	CurrentHash(ctx context.Context, cfg *config.Config) (string, error)
}

// FetchResult contains the result of a fetch operation
type FetchResult struct {
	// Registry is the parsed registry data in unified ServerRegistry format
	Registry *registry.ServerRegistry

	// Hash is the SHA256 hash of the serialized data for change detection
	Hash string

	// ServerCount is the number of servers found in the registry data
	ServerCount int

	// Format indicates the original format of the source data
	Format string
}

// NewFetchResult creates a new FetchResult from a ServerRegistry instance and pre-calculated hash
// The hash should be calculated by the source handler to ensure consistency with CurrentHash
func NewFetchResult(reg *registry.ServerRegistry, hash string, format string) *FetchResult {
	serverCount := 0
	if reg != nil {
		serverCount = len(reg.Servers)
	}

	return &FetchResult{
		Registry:    reg,
		Hash:        hash,
		ServerCount: serverCount,
		Format:      format,
	}
}

// SourceHandlerFactory creates source handlers based on source type
type SourceHandlerFactory interface {
	// CreateHandler creates a source handler for the given source type
	CreateHandler(sourceType string) (SourceHandler, error)
}

// DefaultSourceDataValidator is the default implementation of SourceValidator
type DefaultSourceDataValidator struct{}

// NewSourceDataValidator creates a new default source validator
func NewSourceDataValidator() SourceDataValidator {
	return &DefaultSourceDataValidator{}
}

// ValidateData validates raw data and returns a parsed ServerRegistry
func (*DefaultSourceDataValidator) ValidateData(data []byte, format string) (*registry.ServerRegistry, error) {
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

// validateToolhiveFormatAndParse validates data against ToolHive registry format and returns parsed ServerRegistry
func validateToolhiveFormatAndParse(data []byte) (*registry.ServerRegistry, error) {
	// Use the existing schema validation from toolhive package
	if err := toolhiveregistry.ValidateRegistrySchema(data); err != nil {
		return nil, err
	}

	// Parse the validated data as ToolHive Registry
	var toolhiveReg toolhivetypes.Registry
	if err := json.Unmarshal(data, &toolhiveReg); err != nil {
		return nil, fmt.Errorf("failed to parse ToolHive registry format: %w", err)
	}

	// Convert to ServerRegistry using constructor
	serverReg, err := registry.NewServerRegistryFromToolhive(&toolhiveReg)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to ServerRegistry: %w", err)
	}

	return serverReg, nil
}

// validateUpstreamFormatAndParse validates data against upstream registry format and returns ServerRegistry
func validateUpstreamFormatAndParse(data []byte) (*registry.ServerRegistry, error) {
	// Parse as upstream ServerResponse array to validate structure
	var responses []upstreamv0.ServerResponse
	if err := json.Unmarshal(data, &responses); err != nil {
		return nil, fmt.Errorf("invalid upstream format: %w", err)
	}

	// Basic validation - ensure we have at least one server and required fields
	if len(responses) == 0 {
		return nil, fmt.Errorf("upstream registry must contain at least one server")
	}

	// Extract ServerJSON from responses for validation and conversion
	servers := make([]upstreamv0.ServerJSON, len(responses))
	for i, response := range responses {
		servers[i] = response.Server
		if response.Server.Name == "" {
			return nil, fmt.Errorf("server at index %d: name is required", i)
		}
		if response.Server.Description == "" {
			return nil, fmt.Errorf("server at index %d (%s): description is required", i, response.Server.Name)
		}
	}

	// Wrap in ServerRegistry using constructor
	return registry.NewServerRegistryFromUpstream(servers), nil
}
