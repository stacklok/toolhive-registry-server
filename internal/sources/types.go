package sources

import (
	"context"
	"encoding/json"
	"fmt"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	toolhiveregistry "github.com/stacklok/toolhive/pkg/registry"
	"github.com/stacklok/toolhive/pkg/registry/converters"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/types"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// SourceDataValidator is an interface for validating registry source configurations
type SourceDataValidator interface {
	// ValidateData validates raw data and returns a parsed UpstreamRegistry
	ValidateData(data []byte, format string) (*toolhivetypes.UpstreamRegistry, error)
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
// The hash should be calculated by the source handler to ensure consistency with CurrentHash
func NewFetchResult(reg *toolhivetypes.UpstreamRegistry, hash string, format string) *FetchResult {
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

// ValidateData validates raw data and returns a parsed UpstreamRegistry
func (*DefaultSourceDataValidator) ValidateData(data []byte, format string) (*toolhivetypes.UpstreamRegistry, error) {
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

	// Wrap in UpstreamRegistry using constructor
	return converters.NewUpstreamRegistryFromUpstreamServers(servers), nil
}
