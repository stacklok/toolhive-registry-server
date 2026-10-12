package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	v0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"gopkg.in/yaml.v3"

	"github.com/stacklok/toolhive-registry-server/internal/registry"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// Upstream implements the existing upstream MCP API pagination protocol. It is
// also used by the standalone compatibility adapter. Fetcher adds full payload
// validation; this low-level client alone does not establish a valid snapshot.
type Upstream struct{ client HTTPClient }

// NewUpstream borrows a non-nil client; it never closes it or starts background work.
func NewUpstream(client HTTPClient) (*Upstream, error) {
	if client == nil {
		return nil, persistence.ErrInvalid
	}
	return &Upstream{client: client}, nil
}

// Validate checks the upstream's OpenAPI identity before fetching pages.
func (h *Upstream) Validate(ctx context.Context, endpoint string) error {
	data, err := h.client.Get(ctx, strings.TrimSuffix(endpoint, "/")+"/openapi.yaml")
	if err != nil {
		return fmt.Errorf("failed to fetch /openapi.yaml: %w", err)
	}
	var spec map[string]any
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return fmt.Errorf("failed to parse /openapi.yaml")
	}
	info, ok := spec["info"].(map[string]any)
	if !ok {
		return fmt.Errorf("/openapi.yaml missing 'info' section")
	}
	version, ok := info["version"].(string)
	if !ok {
		return fmt.Errorf("/openapi.yaml info section missing 'version' field")
	}
	if version != "1.0.0" {
		return fmt.Errorf("/openapi.yaml version is %s, expected 1.0.0", version)
	}
	description, ok := info["description"].(string)
	if !ok {
		return fmt.Errorf("/openapi.yaml info section missing 'description' field")
	}
	if !strings.Contains(description, "https://github.com/modelcontextprotocol/registry") {
		return fmt.Errorf("/openapi.yaml description does not contain expected GitHub URL")
	}
	return nil
}

// Fetch retrieves every server page, failing rather than returning partial data.
// The upstream MCP API supplies servers only; file/Git snapshots support all kinds.
func (h *Upstream) Fetch(ctx context.Context, endpoint string) (model.Snapshot, error) {
	var snapshot model.Snapshot
	snapshot.Schema = registry.UpstreamRegistrySchemaURL
	snapshot.Version = registry.UpstreamRegistryVersion
	snapshot.Meta.LastUpdated = time.Now().UTC().Format(time.RFC3339)
	snapshot.Data.Servers = []model.Server{}
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 1000; page++ {
		if err := ctx.Err(); err != nil {
			return model.Snapshot{}, err
		}
		address := strings.TrimSuffix(endpoint, "/") + "/v0.1/servers?limit=100"
		if cursor != "" {
			address += "&cursor=" + url.QueryEscape(cursor)
		}
		data, err := h.client.Get(ctx, address)
		if err != nil {
			return model.Snapshot{}, err
		}
		var response v0.ServerListResponse
		if err := json.Unmarshal(data, &response); err != nil {
			return model.Snapshot{}, fmt.Errorf("failed to parse response page")
		}
		if response.Servers == nil {
			return model.Snapshot{}, fmt.Errorf("response page missing servers array")
		}
		if len(snapshot.Data.Servers)+len(response.Servers) > 100000 {
			return model.Snapshot{}, fmt.Errorf("total servers would exceed maximum")
		}
		for _, server := range response.Servers {
			snapshot.Data.Servers = append(snapshot.Data.Servers, server.Server)
		}
		cursor = response.Metadata.NextCursor
		if cursor == "" {
			return snapshot, nil
		}
		if seen[cursor] {
			return model.Snapshot{}, fmt.Errorf("repeated pagination cursor")
		}
		seen[cursor] = true
	}
	return model.Snapshot{}, fmt.Errorf("pagination exceeded maximum pages")
}
