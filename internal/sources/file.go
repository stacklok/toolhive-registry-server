package sources

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// fileRegistryHandler handles registry data from local files
type fileRegistryHandler struct {
	validator RegistryDataValidator
}

// NewFileRegistryHandler creates a new file registry handler
func NewFileRegistryHandler() RegistryHandler {
	return &fileRegistryHandler{
		validator: NewRegistryDataValidator(),
	}
}

// Validate validates the file registry configuration
func (*fileRegistryHandler) Validate(regCfg *config.RegistryConfig) error {
	if regCfg == nil {
		return fmt.Errorf("registry configuration cannot be nil")
	}

	if regCfg.File == nil {
		return fmt.Errorf("file configuration is required")
	}

	if regCfg.File.Path == "" {
		return fmt.Errorf("file path cannot be empty")
	}

	return nil
}

// FetchRegistry retrieves registry data from the local file
func (h *fileRegistryHandler) FetchRegistry(ctx context.Context, regCfg *config.RegistryConfig) (*FetchResult, error) {
	// Fetch file data
	data, hash, err := h.fetchFileData(ctx, regCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch file data: %w", err)
	}

	// Validate and parse data
	reg, err := h.validator.ValidateData(data, regCfg.Format)
	if err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Return result
	return NewFetchResult(reg, hash, regCfg.Format), nil
}

// fetchFileData reads the file and calculates its hash
func (h *fileRegistryHandler) fetchFileData(_ context.Context, regCfg *config.RegistryConfig) ([]byte, string, error) {
	// Validate registry configuration
	if err := h.Validate(regCfg); err != nil {
		return nil, "", fmt.Errorf("registry validation failed: %w", err)
	}

	filePath := regCfg.File.Path

	// Read the file
	//nolint:gosec // File path comes from user configuration, this is expected behavior
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", fmt.Errorf("file not found: %s", filePath)
		}
		return nil, "", fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	// Calculate hash
	hash := fmt.Sprintf("%x", sha256.Sum256(data))

	return data, hash, nil
}

// CurrentHash returns the current hash of the file without performing a full parse
func (h *fileRegistryHandler) CurrentHash(ctx context.Context, regCfg *config.RegistryConfig) (string, error) {
	// For file sources, we read and hash the file
	// This is nearly as expensive as a full fetch, but maintains the interface
	_, hash, err := h.fetchFileData(ctx, regCfg)
	if err != nil {
		return "", err
	}

	return hash, nil
}
