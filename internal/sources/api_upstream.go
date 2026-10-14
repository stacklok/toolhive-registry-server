package sources

// TODO: Future optimization - Incremental sync support
// Currently this implementation fetches all servers on every sync (full replacement).
// Future enhancement should:
// - Add updatedSince parameter to FetchRegistry (use /v0.1/servers?updated_since={timestamp})
// - Modify storage layer to support UPSERT/merge instead of full replacement
// - Optimize change detection to avoid full fetch (use ETag/Last-Modified headers)
// This would significantly reduce bandwidth and processing for large registries.
// See: https://github.com/stacklok/toolhive-registry-server/issues/XXX (create issue)

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	v0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	toolhivetypes "github.com/stacklok/toolhive-core/registry/types"
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
// API Format: /v0.1/servers (paginated list)
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

// Validate validates that the endpoint speaks the upstream MCP Registry v0.1 API
// by requesting a single-item page of /v0.1/servers and checking that the response
// is a server list. This probes the exact behavior FetchRegistry depends on, so any
// compliant implementation is accepted, not just the official registry.
func (h *upstreamAPIHandler) Validate(ctx context.Context, endpoint string) error {
	probeURL := endpoint + "/v0.1/servers?limit=1"

	data, err := h.httpClient.Get(ctx, probeURL)
	if err != nil {
		return fmt.Errorf("failed to fetch /v0.1/servers: %w", err)
	}

	// Check the 'servers' field explicitly: unmarshalling arbitrary JSON objects
	// into ServerListResponse would succeed with an empty list.
	var probe struct {
		Servers json.RawMessage `json:"servers"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return fmt.Errorf("/v0.1/servers did not return a JSON object: %w", err)
	}
	if !bytes.HasPrefix(bytes.TrimSpace(probe.Servers), []byte("[")) {
		return fmt.Errorf("/v0.1/servers response is missing a 'servers' array")
	}

	var response v0.ServerListResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return fmt.Errorf("/v0.1/servers response is not a valid server list: %w", err)
	}

	return nil
}

// FetchRegistry retrieves registry data from the upstream MCP Registry API endpoint
// It fetches all servers via pagination and converts them to ToolHive's UpstreamRegistry format
func (h *upstreamAPIHandler) FetchRegistry(ctx context.Context, regCfg *config.SourceConfig) (*FetchResult, error) {
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
	return NewFetchResult(upstreamReg, hash), nil
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
func getBaseURL(regCfg *config.SourceConfig) string {
	baseURL := regCfg.API.Endpoint

	// Remove trailing slash
	if len(baseURL) > 0 && baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}

	return baseURL
}
