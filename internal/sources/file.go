package sources

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"time"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/httpclient"
)

const (
	// DefaultURLTimeout is the default timeout for URL requests
	DefaultURLTimeout = 30 * time.Second
)

// fileRegistryHandler handles registry data from local files or URLs
type fileRegistryHandler struct {
	validator  RegistryDataValidator
	httpClient httpclient.Client
	cfg        *config.Config
}

// NewFileRegistryHandler creates a new file registry handler
func NewFileRegistryHandler(cfg *config.Config) RegistryHandler {
	return &fileRegistryHandler{
		validator:  NewRegistryDataValidator(),
		httpClient: httpclient.NewDefaultClient(DefaultURLTimeout),
		cfg:        cfg,
	}
}

// NewFileRegistryHandlerWithClient creates a new file registry handler with a custom HTTP client
// This is useful for testing
func NewFileRegistryHandlerWithClient(client httpclient.Client, cfg *config.Config) RegistryHandler {
	return &fileRegistryHandler{
		validator:  NewRegistryDataValidator(),
		httpClient: client,
		cfg:        cfg,
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

	// Exactly one of Path or URL must be specified
	hasPath := regCfg.File.Path != ""
	hasURL := regCfg.File.URL != ""

	if !hasPath && !hasURL {
		return fmt.Errorf("file path or url cannot be empty")
	}
	if hasPath && hasURL {
		return fmt.Errorf("file path and url are mutually exclusive")
	}

	return nil
}

// isURLSource returns true if the configuration uses a URL source
func (*fileRegistryHandler) isURLSource(regCfg *config.RegistryConfig) bool {
	return regCfg.File != nil && regCfg.File.URL != ""
}

// FetchRegistry retrieves registry data from a local file or URL
func (h *fileRegistryHandler) FetchRegistry(ctx context.Context, regCfg *config.RegistryConfig) (*FetchResult, error) {
	// Fetch data from appropriate source
	data, hash, err := h.fetchData(ctx, regCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch data: %w", err)
	}

	// Validate and parse data
	reg, err := h.validator.ValidateData(data, regCfg.Format)
	if err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Return result
	return NewFetchResult(reg, hash, regCfg.Format), nil
}

// fetchData routes to the appropriate fetch method based on configuration
func (h *fileRegistryHandler) fetchData(ctx context.Context, regCfg *config.RegistryConfig) ([]byte, string, error) {
	// Validate registry configuration
	if err := h.Validate(regCfg); err != nil {
		return nil, "", fmt.Errorf("registry validation failed: %w", err)
	}

	if h.isURLSource(regCfg) {
		return h.fetchURLData(ctx, regCfg)
	}
	return h.fetchLocalFileData(regCfg)
}

// fetchLocalFileData reads the local file and calculates its hash
func (*fileRegistryHandler) fetchLocalFileData(regCfg *config.RegistryConfig) ([]byte, string, error) {
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

// fetchURLData fetches the registry file from a URL and calculates its hash
func (h *fileRegistryHandler) fetchURLData(ctx context.Context, regCfg *config.RegistryConfig) ([]byte, string, error) {
	fileURL := regCfg.File.URL

	// Create HTTP client with configured timeout if specified
	client := h.httpClient
	if regCfg.File.Timeout != "" {
		timeout, err := time.ParseDuration(regCfg.File.Timeout)
		if err != nil {
			return nil, "", fmt.Errorf("invalid timeout: %w", err)
		}
		client = httpclient.NewDefaultClient(timeout)
	}

	// Fetch data from URL
	data, err := client.Get(ctx, fileURL)
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch URL %s: %w", fileURL, err)
	}

	// Calculate hash
	hash := fmt.Sprintf("%x", sha256.Sum256(data))

	return data, hash, nil
}

// CurrentHash returns the current hash of the source without performing a full parse
func (h *fileRegistryHandler) CurrentHash(ctx context.Context, regCfg *config.RegistryConfig) (string, error) {
	// For file/URL sources, we read and hash the content
	// This is nearly as expensive as a full fetch, but maintains the interface
	_, hash, err := h.fetchData(ctx, regCfg)
	if err != nil {
		return "", err
	}

	return hash, nil
}
