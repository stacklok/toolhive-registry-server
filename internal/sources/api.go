package sources

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/httpclient"
)

// APISourceHandler handles registry data from API endpoints
// It validates the Upstream format and delegates to the appropriate handler
type APISourceHandler struct {
	httpClient      httpclient.Client
	validator       SourceDataValidator
	upstreamHandler *UpstreamAPIHandler
}

// NewAPISourceHandler creates a new API source handler
func NewAPISourceHandler() *APISourceHandler {
	httpClient := httpclient.NewDefaultClient(0) // Use default timeout

	return &APISourceHandler{
		httpClient:      httpClient,
		validator:       NewSourceDataValidator(),
		upstreamHandler: NewUpstreamAPIHandler(httpClient),
	}
}

// Validate validates the API source configuration
func (*APISourceHandler) Validate(source *config.SourceConfig) error {
	if source.Type != config.SourceTypeAPI {
		return fmt.Errorf("invalid source type: expected %s, got %s",
			config.SourceTypeAPI, source.Type)
	}

	if source.Format != "" && source.Format != config.SourceFormatUpstream {
		return fmt.Errorf("unsupported format: expected %s or empty, got %s",
			config.SourceFormatUpstream, source.Format)
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
// It validates the Upstream format and delegates to the appropriate handler
func (h *APISourceHandler) FetchRegistry(ctx context.Context, cfg *config.Config) (*FetchResult, error) {
	logger := log.FromContext(ctx)

	// Validate source configuration
	if err := h.Validate(&cfg.Source); err != nil {
		return nil, fmt.Errorf("source validation failed: %w", err)
	}

	// Validate Upstream format and get appropriate handler
	handler, err := h.validateUstreamFormat(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("upstream format validation failed: %w", err)
	}

	logger.Info("Validated Upstream format, delegating to handler")

	// Delegate to the appropriate handler
	return handler.FetchRegistry(ctx, cfg)
}

// CurrentHash returns the current hash of the API response
func (h *APISourceHandler) CurrentHash(ctx context.Context, cfg *config.Config) (string, error) {
	// Validate source configuration
	if err := h.Validate(&cfg.Source); err != nil {
		return "", fmt.Errorf("source validation failed: %w", err)
	}

	// Validate Upstream format and get appropriate handler
	handler, err := h.validateUstreamFormat(ctx, cfg)
	if err != nil {
		return "", fmt.Errorf("upstream format validation failed: %w", err)
	}

	// Delegate to the appropriate handler
	return handler.CurrentHash(ctx, cfg)
}

// validateUstreamFormat validates the Upstream format and returns the appropriate handler
func (h *APISourceHandler) validateUstreamFormat(
	ctx context.Context,
	cfg *config.Config,
) (*UpstreamAPIHandler, error) {
	logger := log.FromContext(ctx)
	endpoint := h.getBaseURL(cfg)

	// Try upstream format (/openapi.yaml)
	upstreamErr := h.upstreamHandler.Validate(ctx, endpoint)
	if upstreamErr == nil {
		logger.Info("Validated as upstream MCP Registry format")
		return h.upstreamHandler, nil
	}
	logger.V(1).Info("Upstream format validation failed", "error", upstreamErr.Error())

	return nil, fmt.Errorf("unable to validate Upstream format")
}

// getBaseURL extracts and normalizes the base URL
func (*APISourceHandler) getBaseURL(cfg *config.Config) string {
	baseURL := cfg.Source.API.Endpoint

	// Remove trailing slash
	if len(baseURL) > 0 && baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}

	return baseURL
}
