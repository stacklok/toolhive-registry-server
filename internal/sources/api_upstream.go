package sources

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/httpclient"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
)

// upstreamAPIHandler retains standalone behavior while sharing the protocol client.
type upstreamAPIHandler struct{ *ingest.Upstream }

// NewUpstreamAPIHandler creates an upstream MCP Registry API handler.
func NewUpstreamAPIHandler(client httpclient.Client) (*upstreamAPIHandler, error) {
	upstream, err := ingest.NewUpstream(client)
	if err != nil {
		return nil, err
	}
	return &upstreamAPIHandler{upstream}, nil
}

// FetchRegistry retrieves all pages. The legacy acceptance rules remain unchanged.
func (h *upstreamAPIHandler) FetchRegistry(ctx context.Context, cfg *config.SourceConfig) (*FetchResult, error) {
	snapshot, err := h.Fetch(ctx, cfg.API.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch servers: %w", err)
	}
	data, err := json.Marshal(snapshot.Data)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	return NewFetchResult(&snapshot, fmt.Sprintf("%x", hash)), nil
}
