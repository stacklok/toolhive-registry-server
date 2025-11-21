package sources

import (
	"context"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/httpclient"
)

// upstreamAPIHandler handles registry data from upstream MCP Registry API endpoints
// API Format: /v0/servers (paginated list), /v0/servers/{name}/versions, /openapi.yaml
// Phase 2 implementation - currently validates format but does not fetch data
type upstreamAPIHandler struct {
	httpClient httpclient.Client
	validator  SourceDataValidator
}

// NewUpstreamAPIHandler creates a new upstream MCP Registry API handler
func NewUpstreamAPIHandler(httpClient httpclient.Client) *upstreamAPIHandler {
	return &upstreamAPIHandler{
		httpClient: httpClient,
		validator:  NewSourceDataValidator(),
	}
}

// Validate validates that the endpoint is an upstream MCP Registry
// by checking /openapi.yaml for expected version and description
func (h *upstreamAPIHandler) Validate(ctx context.Context, endpoint string) error {
	openapiURL := endpoint + "/openapi.yaml"

	data, err := h.httpClient.Get(ctx, openapiURL)
	if err != nil {
		return fmt.Errorf("failed to fetch /openapi.yaml: %w", err)
	}

	// Parse YAML into a map
	var openapiSpec map[string]interface{}
	if err := yaml.Unmarshal(data, &openapiSpec); err != nil {
		return fmt.Errorf("failed to parse /openapi.yaml: %w", err)
	}

	// Check for 'info' section
	info, ok := openapiSpec["info"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("/openapi.yaml missing 'info' section")
	}

	// Check version is 1.0.0
	version, ok := info["version"].(string)
	if !ok {
		return fmt.Errorf("/openapi.yaml info section missing 'version' field")
	}
	if version != "1.0.0" {
		return fmt.Errorf("/openapi.yaml version is %s, expected 1.0.0", version)
	}

	// Check description contains GitHub URL
	description, ok := info["description"].(string)
	if !ok {
		return fmt.Errorf("/openapi.yaml info section missing 'description' field")
	}

	expectedGitHubURL := "https://github.com/modelcontextprotocol/registry"
	if !strings.Contains(description, expectedGitHubURL) {
		return fmt.Errorf("/openapi.yaml description does not contain expected GitHub URL: %s", expectedGitHubURL)
	}

	return nil
}

// FetchRegistry retrieves registry data from the upstream MCP Registry API endpoint
// Phase 2: Not yet implemented - will support pagination and format conversion
func (*upstreamAPIHandler) FetchRegistry(_ context.Context, _ *config.RegistryConfig) (*FetchResult, error) {
	return nil, fmt.Errorf("upstream MCP Registry API support not yet implemented (Phase 2)")
}

// CurrentHash returns the current hash of the API response
// Phase 2: Not yet implemented
func (*upstreamAPIHandler) CurrentHash(_ context.Context, _ *config.RegistryConfig) (string, error) {
	return "", fmt.Errorf("upstream MCP Registry API support not yet implemented (Phase 2)")
}

// TODO Phase 2 implementation:
// - Implement pagination support with cursor handling
// - Fetch /v0/servers with limit/cursor parameters
// - Convert upstream ServerDetail format to ToolHive ImageMetadata
// - Handle version-specific endpoints /v0/servers/{name}/versions
// - Support authentication (Bearer tokens, API keys)
