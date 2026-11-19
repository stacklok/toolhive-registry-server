package persistence_test

import (
	"errors"
	"strings"
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

func TestValidateSnapshotAcceptsServerWithoutSchema(t *testing.T) {
	t.Parallel()
	var snapshot model.Snapshot
	snapshot.Data.Servers = []model.Server{
		{Schema: "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
			Name: "io.test/with-schema", Version: "1.0.0", Description: "has $schema"},
		{Name: "io.test/no-schema", Version: "1.0.0", Description: "registry files may omit $schema"},
	}
	if err := persistence.ValidateSnapshot(&snapshot); err != nil {
		t.Fatalf("server without $schema rejected: %v", err)
	}
	if snapshot.Data.Servers[1].Schema != "" {
		t.Fatalf("validation changed the stored $schema to %q", snapshot.Data.Servers[1].Schema)
	}
	snapshot.Data.Servers[0].Schema = "not a uri"
	if err := persistence.ValidateSnapshot(&snapshot); !errors.Is(err, persistence.ErrInvalid) {
		t.Fatalf("a present but invalid $schema must still fail: %v", err)
	}
}

func TestValidateSnapshotRemembersOnlyValidContent(t *testing.T) {
	t.Parallel()
	server := model.Server{Schema: "https://example.org/schema.json", Name: "com.example/remembered", Version: "1", Description: "ok"}
	valid := model.Snapshot{Data: thvregistry.UpstreamData{Servers: []model.Server{server}}}
	for range 2 {
		if err := persistence.ValidateSnapshot(&valid); err != nil {
			t.Fatalf("valid snapshot: %v", err)
		}
	}
	// A remembered digest never vouches for different content.
	long := server
	long.Description = strings.Repeat("x", 101)
	invalid := model.Snapshot{Data: thvregistry.UpstreamData{Servers: []model.Server{long}}}
	// Identity checks run before the memo: a duplicate of valid content fails.
	duplicate := model.Snapshot{Data: thvregistry.UpstreamData{Servers: []model.Server{server, server}}}
	for _, s := range []*model.Snapshot{&invalid, &invalid, &duplicate} {
		if err := persistence.ValidateSnapshot(s); !errors.Is(err, persistence.ErrInvalid) {
			t.Fatalf("invalid snapshot: %v", err)
		}
	}
}
