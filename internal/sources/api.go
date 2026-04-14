package sources

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/httpclient"
)

// apiRegistryHandler handles registry data from API endpoints
// It validates the Upstream format and delegates to the appropriate handler
type apiRegistryHandler struct {
	httpClient      httpclient.Client
	validator       RegistryDataValidator
	upstreamHandler *upstreamAPIHandler
}

// NewAPIRegistryHandler creates a new API registry handler
func NewAPIRegistryHandler() RegistryHandler {
	httpClient := httpclient.NewDefaultClient(0) // Use default timeout

	return &apiRegistryHandler{
		httpClient:      httpClient,
		validator:       NewRegistryDataValidator(),
		upstreamHandler: NewUpstreamAPIHandler(httpClient),
	}
}

// Validate validates the API registry configuration
func (*apiRegistryHandler) Validate(regCfg *config.SourceConfig) error {
	if regCfg == nil {
		return fmt.Errorf("registry configuration cannot be nil")
	}

	if regCfg.API == nil {
		return fmt.Errorf("api configuration is required")
	}

	if regCfg.API.Endpoint == "" {
		return fmt.Errorf("api endpoint cannot be empty")
	}

	return nil
}

// FetchRegistry retrieves registry data from the API endpoint
// It validates the Upstream format and delegates to the appropriate handler
func (h *apiRegistryHandler) FetchRegistry(ctx context.Context, regCfg *config.SourceConfig) (*FetchResult, error) {
	// Validate registry configuration
	if err := h.Validate(regCfg); err != nil {
		return nil, fmt.Errorf("registry validation failed: %w", err)
	}

	// Validate Upstream format and get appropriate handler
	handler, err := h.validateUstreamFormat(ctx, regCfg)
	if err != nil {
		return nil, fmt.Errorf("upstream format validation failed: %w", err)
	}

	slog.Info("Validated Upstream format, delegating to handler")

	// Delegate to the appropriate handler
	return handler.FetchRegistry(ctx, regCfg)
}

// validateUstreamFormat validates the Upstream format and returns the appropriate handler
func (h *apiRegistryHandler) validateUstreamFormat(
	ctx context.Context,
	regCfg *config.SourceConfig,
) (*upstreamAPIHandler, error) {
	endpoint := h.getBaseURL(regCfg)

	// Try upstream format (/openapi.yaml)
	upstreamErr := h.upstreamHandler.Validate(ctx, endpoint)
	if upstreamErr == nil {
		slog.Info("Validated as upstream MCP Registry format")
		return h.upstreamHandler, nil
	}
	slog.Debug("Upstream format validation failed", "error", upstreamErr.Error())

	return nil, fmt.Errorf("unable to validate Upstream format")
}

// getBaseURL extracts and normalizes the base URL
func (*apiRegistryHandler) getBaseURL(regCfg *config.SourceConfig) string {
	baseURL := regCfg.API.Endpoint

	// Remove trailing slash
	if len(baseURL) > 0 && baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}

	return baseURL
}
