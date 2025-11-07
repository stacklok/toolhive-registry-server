package sources

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"

	"github.com/stacklok/toolhive-registry-server/pkg/config"
)

// FileSourceHandler handles registry data from local files
type FileSourceHandler struct {
	validator SourceDataValidator
}

// NewFileSourceHandler creates a new file source handler
func NewFileSourceHandler() *FileSourceHandler {
	return &FileSourceHandler{
		validator: NewSourceDataValidator(),
	}
}

// Validate validates the file source configuration
func (*FileSourceHandler) Validate(source *config.SourceConfig) error {
	if source.Type != config.SourceTypeFile {
		return fmt.Errorf("invalid source type: expected %s, got %s",
			config.SourceTypeFile, source.Type)
	}

	if source.File == nil {
		return fmt.Errorf("file configuration is required for source type %s",
			config.SourceTypeFile)
	}

	if source.File.Path == "" {
		return fmt.Errorf("file path cannot be empty")
	}

	return nil
}

// FetchRegistry retrieves registry data from the local file
func (h *FileSourceHandler) FetchRegistry(ctx context.Context, registryConfig *config.Config) (*FetchResult, error) {
	// Fetch file data
	data, hash, err := h.fetchFileData(ctx, registryConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch file data: %w", err)
	}

	// Validate and parse data
	reg, err := h.validator.ValidateData(data, registryConfig.Source.Format)
	if err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Return result
	return NewFetchResult(reg, hash, registryConfig.Source.Format), nil
}

// fetchFileData reads the file and calculates its hash
func (h *FileSourceHandler) fetchFileData(ctx context.Context, registryConfig *config.Config) ([]byte, string, error) {
	// Validate source configuration
	if err := h.Validate(&registryConfig.Source); err != nil {
		return nil, "", fmt.Errorf("source validation failed: %w", err)
	}

	filePath := registryConfig.Source.File.Path

	// Read the file
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
func (h *FileSourceHandler) CurrentHash(ctx context.Context, registryConfig *config.Config) (string, error) {
	// For file sources, we read and hash the file
	// This is nearly as expensive as a full fetch, but maintains the interface
	_, hash, err := h.fetchFileData(ctx, registryConfig)
	if err != nil {
		return "", err
	}

	return hash, nil
}
