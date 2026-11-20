package persistence_test

import (
	"errors"
	"strings"
	"testing"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	upstreammodel "github.com/modelcontextprotocol/registry/pkg/model"
	thvregistry "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

func TestValidateStorableSnapshot(t *testing.T) {
	t.Parallel()
	server := func(name, version string, change ...func(*model.Server)) model.Snapshot {
		s := model.Server{Name: name, Version: version}
		for _, c := range change {
			c(&s)
		}
		return model.Snapshot{Data: thvregistry.UpstreamData{Servers: []model.Server{s}}}
	}
	remote := upstreammodel.Transport{Type: "streamable-http", URL: "https://example.org/mcp"}
	pkg := upstreammodel.Package{RegistryType: "oci", Identifier: "example.org/image:1", Transport: upstreammodel.Transport{Type: "stdio"}}
	icon := upstreammodel.Icon{Src: "https://example.org/icon.png"}
	for _, tc := range []struct {
		name     string
		snapshot model.Snapshot
		reason   string // empty: valid
	}{
		{name: "empty"},
		// Accepted although the strict publication check rejects them.
		{name: "no description", snapshot: server("com.example/x", "1")},
		{name: "long description and latest version", snapshot: server("com.example/x", "latest", func(s *model.Server) {
			s.Description = strings.Repeat("d", 500)
		})},
		{name: "name ending in underscore", snapshot: server("com.example/bar_", "1")},
		{name: "namespace starting with a dash", snapshot: server("-example.com/x", "1")},
		{name: "duplicate packages, remotes and icons", snapshot: server("com.example/x", "1", func(s *model.Server) {
			s.Packages = []upstreammodel.Package{pkg, pkg}
			s.Remotes = []upstreammodel.Transport{remote, remote}
			s.Icons = []upstreammodel.Icon{icon, icon}
		})},
		{name: "same identity across kinds", snapshot: model.Snapshot{Data: thvregistry.UpstreamData{
			Servers: []model.Server{{Name: "com.example/x", Version: "1"}},
			Skills:  []thvregistry.Skill{{Name: "com.example/x", Version: "1"}},
			Plugins: []thvregistry.Plugin{{Name: "com.example/x", Version: "1"}},
		}}},
		{name: "skill status in any case", snapshot: model.Snapshot{Data: thvregistry.UpstreamData{
			Skills: []thvregistry.Skill{{Name: "s", Version: "1", Status: "deprecated"}}}}},
		// Rejected: what storage or the read routes cannot hold.
		{name: "empty server name", snapshot: server("", "1"), reason: "name is required"},
		{name: "empty version", snapshot: server("com.example/x", ""), reason: "version is required"},
		{name: "name without slash", snapshot: server("example", "1"), reason: "exactly one '/'"},
		{name: "name with two slashes", snapshot: server("a/b/c", "1"), reason: "exactly one '/'"},
		{name: "empty name part", snapshot: server("com.example/", "1"), reason: "exactly one '/'"},
		{name: "name with whitespace", snapshot: server("com.example/a b", "1"), reason: "whitespace"},
		{name: "name too long", snapshot: server("com.example/"+strings.Repeat("x", persistence.MaxStoredIdentityBytes), "1"),
			reason: "at most 4096 bytes"},
		{name: "NUL in description", snapshot: server("com.example/x", "1", func(s *model.Server) { s.Description = "a\x00b" }),
			reason: "description: text contains a NUL character"},
		{name: "invalid UTF-8 in remote URL", snapshot: server("com.example/x", "1", func(s *model.Server) {
			s.Remotes = []upstreammodel.Transport{{Type: "sse", URL: "https://example.org/\xff"}}
		}), reason: "remotes[0].url: text is not valid UTF-8"},
		{name: "NUL in metadata value", snapshot: server("com.example/x", "1", func(s *model.Server) {
			s.Meta = &upstreamv0.ServerMeta{PublisherProvided: map[string]any{"io.example": map[string]any{"k": "v\x00"}}}
		}), reason: "publisher-provided.io.example.k: text contains a NUL character"},
		{name: "NUL in metadata key", snapshot: server("com.example/x", "1", func(s *model.Server) {
			s.Meta = &upstreamv0.ServerMeta{PublisherProvided: map[string]any{"bad\x00key": "v"}}
		}), reason: "key text contains a NUL character"},
		{name: "duplicate server version", snapshot: model.Snapshot{Data: thvregistry.UpstreamData{
			Servers: []model.Server{{Name: "com.example/x", Version: "1"}, {Name: "com.example/x", Version: "1"}}}},
			reason: "duplicate name and version"},
		{name: "duplicate skill version across namespaces", snapshot: model.Snapshot{Data: thvregistry.UpstreamData{
			Skills: []thvregistry.Skill{{Namespace: "a", Name: "s", Version: "1"}, {Namespace: "b", Name: "s", Version: "1"}}}},
			reason: "duplicate name and version"},
		{name: "unknown plugin status", snapshot: model.Snapshot{Data: thvregistry.UpstreamData{
			Plugins: []thvregistry.Plugin{{Name: "p", Version: "1", Status: "retired"}}}}, reason: "status must be"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := persistence.ValidateStorableSnapshot(&tc.snapshot)
			if tc.reason == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, persistence.ErrInvalid) {
				t.Fatalf("want ErrInvalid, got %v", err)
			}
			var record *persistence.RecordError
			if !errors.As(err, &record) {
				t.Fatalf("want a RecordError naming the record, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("error %q does not contain %q", err, tc.reason)
			}
		})
	}
	if err := persistence.ValidateStorableSnapshot(nil); !errors.Is(err, persistence.ErrInvalid) {
		t.Fatalf("nil snapshot: %v", err)
	}
}

func TestRecordErrorNamesTheRecord(t *testing.T) {
	t.Parallel()
	s := model.Snapshot{Data: thvregistry.UpstreamData{Servers: []model.Server{{Name: "com.example/x", Version: "2.0"}}}}
	s.Data.Servers[0].Title = "\x00"
	err := persistence.ValidateStorableSnapshot(&s)
	want := `invalid definition: server "com.example/x" version "2.0": title: text contains a NUL character`
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %s", err, want)
	}
}
