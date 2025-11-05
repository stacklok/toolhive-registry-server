package sources

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/stacklok/toolhive-registry-server/pkg/config"
	"github.com/stacklok/toolhive-registry-server/pkg/httpclient"
)

// APISourceHandler handles registry data from API endpoints
// It detects the format (ToolHive vs Upstream) and delegates to the appropriate handler
type APISourceHandler struct {
	httpClient      httpclient.Client
	validator       SourceDataValidator
	toolhiveHandler *ToolHiveAPIHandler
	upstreamHandler *UpstreamAPIHandler
}

// NewAPISourceHandler creates a new API source handler
func NewAPISourceHandler() *APISourceHandler {
	httpClient := httpclient.NewDefaultClient(0) // Use default timeout

	return &APISourceHandler{
		httpClient:      httpClient,
		validator:       NewSourceDataValidator(),
		toolhiveHandler: NewToolHiveAPIHandler(httpClient),
		upstreamHandler: NewUpstreamAPIHandler(httpClient),
	}
}

// Validate validates the API source configuration
func (*APISourceHandler) Validate(source *config.SourceConfig) error {
	if source.Type != config.SourceTypeAPI {
		return fmt.Errorf("invalid source type: expected %s, got %s",
			config.SourceTypeAPI, source.Type)
	}

	if source.API == nil {
		return fmt.Errorf("api configuration is required for source type %s",
			config.SourceTypeAPI)
	}

	if source.API.Endpoint == "" {
		return fmt.Errorf("api endpoint cannot be empty")
	}

	return nil
}

// FetchRegistry retrieves registry data from the API endpoint
// It auto-detects the format and delegates to the appropriate handler
func (h *APISourceHandler) FetchRegistry(ctx context.Context, config *config.Config) (*FetchResult, error) {
	logger := log.FromContext(ctx)

	// Validate source configuration
	if err := h.Validate(&config.Source); err != nil {
		return nil, fmt.Errorf("source validation failed: %w", err)
	}

	// Detect format and get appropriate handler
	handler, format, err := h.detectFormatAndGetHandler(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("format detection failed: %w", err)
	}

	logger.Info("Detected API format, delegating to handler",
		"format", format)

	// Delegate to the appropriate handler
	return handler.FetchRegistry(ctx, config)
}

// CurrentHash returns the current hash of the API response
func (h *APISourceHandler) CurrentHash(ctx context.Context, config *config.Config) (string, error) {
	// Validate source configuration
	if err := h.Validate(&config.Source); err != nil {
		return "", fmt.Errorf("source validation failed: %w", err)
	}

	// Detect format and get appropriate handler
	handler, _, err := h.detectFormatAndGetHandler(ctx, config)
	if err != nil {
		return "", fmt.Errorf("format detection failed: %w", err)
	}

	// Delegate to the appropriate handler
	return handler.CurrentHash(ctx, config)
}

// apiFormatHandler is an internal interface for format-specific handlers
type apiFormatHandler interface {
	Validate(ctx context.Context, endpoint string) error
	FetchRegistry(ctx context.Context, config *config.Config) (*FetchResult, error)
	CurrentHash(ctx context.Context, config *config.Config) (string, error)
}

// detectFormatAndGetHandler detects the API format and returns the appropriate handler
func (h *APISourceHandler) detectFormatAndGetHandler(
	ctx context.Context,
	config *config.Config,
) (apiFormatHandler, string, error) {
	logger := log.FromContext(ctx)
	endpoint := h.getBaseURL(config)

	// Try ToolHive format first (/v0/info)
	toolhiveErr := h.toolhiveHandler.Validate(ctx, endpoint)
	if toolhiveErr == nil {
		logger.Info("Validated as ToolHive format")
		return h.toolhiveHandler, "toolhive", nil
	}
	logger.V(1).Info("ToolHive format validation failed", "error", toolhiveErr.Error())

	// Try upstream format (/openapi.yaml)
	upstreamErr := h.upstreamHandler.Validate(ctx, endpoint)
	if upstreamErr == nil {
		logger.Info("Validated as upstream MCP Registry format")
		return h.upstreamHandler, "upstream", nil
	}
	logger.V(1).Info("Upstream format validation failed", "error", upstreamErr.Error())

	return nil, "", fmt.Errorf("unable to detect valid API format (tried toolhive and upstream)")
}

// getBaseURL extracts and normalizes the base URL
func (*APISourceHandler) getBaseURL(config *config.Config) string {
	baseURL := config.Source.API.Endpoint

	// Remove trailing slash
	if len(baseURL) > 0 && baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}

	return baseURL
}
