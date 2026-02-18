package sources

// TODO: Future optimization - Incremental sync support
// Currently this implementation fetches all servers on every sync (full replacement).
// Future enhancement should:
// - Add updatedSince parameter to FetchRegistry (use /v0.1/servers?updated_since={timestamp})
// - Modify storage layer to support UPSERT/merge instead of full replacement
// - Optimize CurrentHash to avoid full fetch (use ETag/Last-Modified headers)
// This would significantly reduce bandwidth and processing for large registries.
// See: https://github.com/stacklok/toolhive-registry-server/issues/XXX (create issue)

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	v0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"
	"gopkg.in/yaml.v3"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/httpclient"
	"github.com/stacklok/toolhive-registry-server/internal/registry"
)

const (
	// maxPaginationPages is the maximum number of pages to fetch to prevent infinite loops
	maxPaginationPages = 1000

	// maxServers is the maximum number of servers to fetch to prevent memory exhaustion
	maxServers = 100000
)

// upstreamAPIHandler handles registry data from upstream MCP Registry API endpoints
// API Format: /v0/servers (paginated list), /v0/servers/{name}/versions, /openapi.yaml
// Phase 2 implementation - currently validates format but does not fetch data
type upstreamAPIHandler struct {
	httpClient httpclient.Client
	validator  RegistryDataValidator
}

// NewUpstreamAPIHandler creates a new upstream MCP Registry API handler
func NewUpstreamAPIHandler(httpClient httpclient.Client) *upstreamAPIHandler {
	return &upstreamAPIHandler{
		httpClient: httpClient,
		validator:  NewRegistryDataValidator(),
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
	var openapiSpec map[string]any
	if err := yaml.Unmarshal(data, &openapiSpec); err != nil {
		return fmt.Errorf("failed to parse /openapi.yaml: %w", err)
	}

	// Check for 'info' section
	info, ok := openapiSpec["info"].(map[string]any)
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
// It fetches all servers via pagination and converts them to ToolHive's UpstreamRegistry format
func (h *upstreamAPIHandler) FetchRegistry(ctx context.Context, regCfg *config.RegistryConfig) (*FetchResult, error) {
	logger := log.FromContext(ctx)
	baseURL := getBaseURL(regCfg)

	// Fetch all servers via pagination
	servers, err := h.fetchAllServers(ctx, baseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch servers: %w", err)
	}

	logger.Info("Fetched all servers from upstream API", "count", len(servers))

	// Convert to UpstreamRegistry format
	upstreamReg := h.buildUpstreamRegistry(servers)

	// Calculate hash
	hash, err := h.calculateHash(upstreamReg)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate hash: %w", err)
	}

	// Return as FetchResult
	return NewFetchResult(upstreamReg, hash, config.SourceFormatUpstream), nil
}

// CurrentHash returns the current hash of the API response
// TODO: Optimize this - could use HEAD request, ETag header, or last-modified header
// For now, perform full fetch to get hash (simple but consistent with git/file handlers)
func (h *upstreamAPIHandler) CurrentHash(ctx context.Context, regCfg *config.RegistryConfig) (string, error) {
	result, err := h.FetchRegistry(ctx, regCfg)
	if err != nil {
		return "", err
	}
	return result.Hash, nil
}

// fetchAllServers performs paginated fetching and returns all ServerJSON objects
func (h *upstreamAPIHandler) fetchAllServers(ctx context.Context, baseURL string) ([]v0.ServerJSON, error) {
	logger := log.FromContext(ctx)
	allServers := []v0.ServerJSON{}
	cursor := ""
	pageCount := 0

	for {
		pageCount++

		// Security: Prevent infinite pagination loops
		if pageCount > maxPaginationPages {
			return nil, fmt.Errorf(
				"pagination exceeded maximum pages (%d), possible infinite loop or malicious upstream",
				maxPaginationPages,
			)
		}

		// Build URL with pagination
		requestURL := fmt.Sprintf("%s/v0.1/servers?limit=100", baseURL)
		if cursor != "" {
			// Security: URL-encode cursor to prevent injection attacks
			requestURL = fmt.Sprintf("%s&cursor=%s", requestURL, url.QueryEscape(cursor))
		}

		logger.V(1).Info("Fetching page", "page", pageCount, "url", requestURL)

		// Fetch page
		data, err := h.httpClient.Get(ctx, requestURL)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch page %d: %w", pageCount, err)
		}

		// Parse response
		var response v0.ServerListResponse
		if err := json.Unmarshal(data, &response); err != nil {
			return nil, fmt.Errorf("failed to parse response page %d: %w", pageCount, err)
		}

		logger.V(1).Info("Parsed page", "page", pageCount, "serversInPage", len(response.Servers))

		// Security: Prevent memory exhaustion from too many servers
		if len(allServers)+len(response.Servers) > maxServers {
			return nil, fmt.Errorf("total servers (%d) would exceed maximum (%d), could cause out of service",
				len(allServers)+len(response.Servers), maxServers)
		}

		// Extract ServerJSON from each ServerResponse
		for _, serverResp := range response.Servers {
			allServers = append(allServers, serverResp.Server)
		}

		// Check if there are more pages
		if response.Metadata.NextCursor == "" {
			logger.Info("Pagination complete", "totalPages", pageCount, "totalServers", len(allServers))
			break
		}

		cursor = response.Metadata.NextCursor
	}

	return allServers, nil
}

// buildUpstreamRegistry converts []ServerJSON to ToolHive's UpstreamRegistry format
func (*upstreamAPIHandler) buildUpstreamRegistry(servers []v0.ServerJSON) *toolhivetypes.UpstreamRegistry {
	return &toolhivetypes.UpstreamRegistry{
		Schema:  registry.UpstreamRegistrySchemaURL,
		Version: registry.UpstreamRegistryVersion,
		Meta: toolhivetypes.UpstreamMeta{
			LastUpdated: time.Now().UTC().Format(time.RFC3339),
		},
		Data: toolhivetypes.UpstreamData{
			Servers: servers,
			Groups:  []toolhivetypes.UpstreamGroup{},
		},
	}
}

// calculateHash computes SHA256 hash of the registry data (servers and groups)
// We hash only the Data field (not the full registry) to ensure consistent hashes
// for the same content, regardless of when the fetch was performed.
// This excludes Meta.LastUpdated which changes on every fetch.
func (*upstreamAPIHandler) calculateHash(reg *toolhivetypes.UpstreamRegistry) (string, error) {
	// Serialize only the data to JSON (excludes metadata like LastUpdated timestamp)
	data, err := json.Marshal(reg.Data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal registry data: %w", err)
	}

	// Compute SHA256
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash), nil
}

// getBaseURL extracts and normalizes the base URL from the registry configuration
func getBaseURL(regCfg *config.RegistryConfig) string {
	baseURL := regCfg.API.Endpoint

	// Remove trailing slash
	if len(baseURL) > 0 && baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}

	return baseURL
}
