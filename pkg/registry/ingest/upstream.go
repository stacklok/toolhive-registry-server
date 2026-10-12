package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	v0 "github.com/modelcontextprotocol/registry/pkg/api/v0"

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

// Validate checks that the endpoint speaks the upstream MCP Registry v0.1 API.
func (h *Upstream) Validate(ctx context.Context, endpoint string) error {
	data, err := h.client.Get(ctx, strings.TrimSuffix(endpoint, "/")+"/v0.1/servers?limit=1")
	if err != nil {
		return fmt.Errorf("failed to fetch /v0.1/servers: %w", err)
	}

	// Unmarshalling a JSON object without "servers" would otherwise succeed with
	// an empty list, so verify the field is an array before decoding the response.
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
