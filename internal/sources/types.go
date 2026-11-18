package sources

import (
	"encoding/json"
	"fmt"

	toolhivetypes "github.com/stacklok/toolhive-core/registry/types"
)

// RegistryDataValidator is an interface for validating registry source configurations
type RegistryDataValidator interface {
	// ValidateData validates raw data and returns a parsed UpstreamRegistry
	ValidateData(data []byte) (*toolhivetypes.UpstreamRegistry, error)
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
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}

	return validateUpstreamFormatAndParse(data)
}

// validateUpstreamFormatAndParse validates data against upstream registry format and returns UpstreamRegistry
func validateUpstreamFormatAndParse(data []byte) (*toolhivetypes.UpstreamRegistry, error) {
	// Validate using toolhive's upstream registry schema validator
	if err := toolhivetypes.ValidateUpstreamRegistryBytes(data); err != nil {
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
