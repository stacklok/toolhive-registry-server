package persistence_test

import (
	"errors"
	"testing"

	upstreammodel "github.com/modelcontextprotocol/registry/pkg/model"
	thvregistry "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

func TestValidateSnapshot(t *testing.T) {
	t.Parallel()
	theme := "light"
	for _, tc := range []struct {
		name     string
		snapshot model.Snapshot
		invalid  bool
	}{
		{name: "empty"},
		{name: "skill", snapshot: model.Snapshot{Data: thvregistry.UpstreamData{Skills: []thvregistry.Skill{{Namespace: "com.example", Name: "item", Version: "custom", Description: "ok"}}}}},
		{name: "duplicate skill version", snapshot: model.Snapshot{Data: thvregistry.UpstreamData{Skills: []thvregistry.Skill{
			{Namespace: "com.example", Name: "item", Version: "1", Description: "ok"}, {Namespace: "com.example", Name: "item", Version: "1", Description: "ok"}}}}, invalid: true},
		{name: "shared exact name is outside skill format", snapshot: model.Snapshot{Data: thvregistry.UpstreamData{
			Servers: []model.Server{{Schema: "https://example.org/schema.json", Name: "com.example/item", Version: "1", Description: "ok"}},
			Skills:  []thvregistry.Skill{{Namespace: "com.example", Name: "com.example/item", Version: "1", Description: "ok"}},
			Plugins: []thvregistry.Plugin{{Namespace: "com.example", Name: "com.example/item", Version: "1", Description: "ok"}},
		}}, invalid: true},
		{name: "duplicate remote", snapshot: model.Snapshot{Data: thvregistry.UpstreamData{Servers: []model.Server{{
			Schema: "https://example.org/schema.json", Name: "com.example/item", Version: "1", Description: "ok",
			Remotes: []upstreammodel.Transport{{Type: "streamable-http", URL: "https://example.org/mcp"}, {Type: "streamable-http", URL: "https://example.org/mcp"}},
		}}}}, invalid: true},
		{name: "default icon theme collision", snapshot: model.Snapshot{Data: thvregistry.UpstreamData{Servers: []model.Server{{
			Schema: "https://example.org/schema.json", Name: "com.example/item", Version: "1", Description: "ok",
			Icons: []upstreammodel.Icon{{Src: "https://example.org/icon.png"}, {Src: "https://example.org/icon.png", Theme: &theme}},
		}}}}, invalid: true},
		{name: "invalid skill", snapshot: model.Snapshot{Data: thvregistry.UpstreamData{Skills: []thvregistry.Skill{{Name: "item", Version: "1"}}}}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := persistence.ValidateSnapshot(&tc.snapshot)
			if (err != nil) != tc.invalid || tc.invalid && !errors.Is(err, persistence.ErrInvalid) {
				t.Fatalf("validation: %v", err)
			}
		})
	}
}
