package sources

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/httpclient"
)

// apiRegistryHandler handles registry data from API endpoints
// It validates the Upstream format and delegates to the appropriate handler
type apiRegistryHandler struct {
	validator RegistryDataValidator
}

// NewAPIRegistryHandler creates a new API registry handler
func NewAPIRegistryHandler() RegistryHandler {
	return &apiRegistryHandler{
		validator: NewRegistryDataValidator(),
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

	// Build the upstream handler with an HTTP client whose timeout honors the
	// optional per-source override, falling back to DefaultAPITimeout.
	handler, err := h.newUpstreamHandler(regCfg)
	if err != nil {
		return nil, err
	}

	// Validate Upstream format before delegating
	if err := h.validateUpstreamFormat(ctx, handler, regCfg); err != nil {
		return nil, fmt.Errorf("upstream format validation failed: %w", err)
	}

	slog.Info("Validated Upstream format, delegating to handler")

	// Delegate to the appropriate handler
	return handler.FetchRegistry(ctx, regCfg)
}

// newUpstreamHandler builds an upstream API handler backed by an HTTP client whose
// timeout honors the optional per-source override, defaulting to httpclient.DefaultTimeout.
// Bounds on the override are enforced at config load (see config.validateAPIConfig).
func (*apiRegistryHandler) newUpstreamHandler(regCfg *config.SourceConfig) (*upstreamAPIHandler, error) {
	timeout := httpclient.DefaultTimeout
	if regCfg.API.Timeout != "" {
		parsed, err := time.ParseDuration(regCfg.API.Timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid api timeout %q: %w", regCfg.API.Timeout, err)
		}
		timeout = parsed
	}

	return NewUpstreamAPIHandler(httpclient.NewDefaultClient(timeout)), nil
}

// validateUpstreamFormat validates the endpoint speaks the upstream MCP Registry format
// (by checking /openapi.yaml via the given handler)
func (h *apiRegistryHandler) validateUpstreamFormat(
	ctx context.Context,
	handler *upstreamAPIHandler,
	regCfg *config.SourceConfig,
) error {
	endpoint := h.getBaseURL(regCfg)

	if err := handler.Validate(ctx, endpoint); err != nil {
		slog.Debug("Upstream format validation failed", "error", err.Error())
		return fmt.Errorf("unable to validate Upstream format")
	}

	slog.Info("Validated as upstream MCP Registry format")
	return nil
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
