package conformance

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	upstream "github.com/modelcontextprotocol/registry/pkg/api/v0"
	upstreammodel "github.com/modelcontextprotocol/registry/pkg/model"
	thvregistry "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/formats"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

const (
	testVersion           = "1.0.0"
	testCustomVersion     = "custom"
	testSemverTie         = "v1.0.0"
	testSkillName         = "item"
	testSkillDescription  = "skill description"
	testLiteralLatest     = "latest"
	testNamespace         = "com.example"
	testMatchingTitle     = "Matching title"
	testSearchTerm        = "Matching"
	testMetadataKey       = "key"
	testMetadataValue     = "value"
	testPayloadData       = "data"
	testStatusActive      = "active"
	testStatusDeprecated  = "deprecated"
	testPluginDescription = "plugin description"
	testDefaultSkillName  = "default-skill"
	testDefaultPluginName = "default-plugin"
	testServerName        = "com.example/item"
	testManagedKind       = "managed"
	testInlineName        = "inline"
)

// EntriesFactory constructs a fresh definitions and entries backend per case.
type EntriesFactory func(*testing.T) (persistence.Sources, persistence.Entries)

// EntriesPairFactory returns two handles over a fresh shared backend state.
type EntriesPairFactory func(*testing.T) (persistence.Sources, persistence.Entries, persistence.Entries)

// RunEntries exercises a real backend's public entry/snapshot contracts.
//
//nolint:gocyclo,lll // Each independent subtest checks full multi-kind state transitions and call sites.
func RunEntries(t *testing.T, factory EntriesFactory) {
	t.Helper()
	t.Run("retained payload replacement", func(t *testing.T) {
		t.Parallel()
		sources, entries := factory(t)
		ctx := t.Context()
		source := newEntrySource(t, sources, "replace")
		original := model.Snapshot{}
		server := conformanceServer("1")
		server.Packages = []upstreammodel.Package{{RegistryType: "npm", Identifier: "foo", Transport: upstreammodel.Transport{Type: "stdio"}}}
		original.Data.Servers = []model.Server{server}
		skill := conformanceSkill("1")
		skill.Status = testStatusDeprecated
		skill.Metadata = map[string]any{"old": testPayloadData}
		original.Data.Skills = []thvregistry.Skill{skill}
		plugin := conformancePlugin("1")
		plugin.Status = testStatusDeprecated
		original.Data.Plugins = []thvregistry.Plugin{plugin}
		if err := entries.ReplaceSnapshot(ctx, source, original); err != nil {
			t.Fatal(err)
		}
		ids := map[persistence.EntryKind]string{}
		for _, item := range []struct {
			kind persistence.EntryKind
			name string
		}{{persistence.ServerKind, server.Name}, {persistence.SkillKind, skill.Name}, {persistence.PluginKind, plugin.Name}} {
			got, err := entries.GetEntry(ctx, source.ID, item.kind, item.name, persistence.VersionSelector{Exact: "1"})
			if err != nil {
				t.Fatal(err)
			}
			ids[item.kind] = got.ID
		}
		server.Packages = []upstreammodel.Package{{RegistryType: "pypi", Identifier: "foo", Transport: upstreammodel.Transport{Type: "stdio"}}}
		server.Description = "new server"
		skill.Namespace = "org.changed"
		skill.Status = ""
		skill.Title = "New skill"
		skill.Metadata = map[string]any{"new": testPayloadData}
		skill.Meta = map[string]any{"extension": true}
		skill.Repository = &thvregistry.SkillRepository{URL: "https://example.org/skill"}
		skill.Icons = []thvregistry.SkillIcon{{Src: "https://example.org/skill.png"}}
		skill.Packages = []thvregistry.SkillPackage{{RegistryType: "oci", Identifier: "example/skill:1"}}
		skill.Provenance = &thvregistry.Provenance{RepositoryURI: "https://example.org/source"}
		plugin.Namespace = "org.changed"
		plugin.Status = ""
		plugin.Title = "New plugin"
		plugin.Metadata = map[string]any{"new": testPayloadData}
		plugin.Meta = map[string]any{"extension": true}
		plugin.Repository = &thvregistry.SkillRepository{URL: "https://example.org/plugin"}
		plugin.Icons = []thvregistry.SkillIcon{{Src: "https://example.org/plugin.png"}}
		plugin.Packages = []thvregistry.SkillPackage{{RegistryType: "git", URL: "https://example.org/plugin.git", Commit: "abc"}}
		next := model.Snapshot{}
		next.Data.Servers = []model.Server{server}
		next.Data.Skills = []thvregistry.Skill{skill}
		next.Data.Plugins = []thvregistry.Plugin{plugin}
		if err := entries.ReplaceSnapshot(ctx, source, next); err != nil {
			t.Fatal(err)
		}
		gotServer, err := entries.GetEntry(ctx, source.ID, persistence.ServerKind, server.Name, persistence.VersionSelector{Exact: "1"})
		if err != nil || gotServer.ID != ids[persistence.ServerKind] {
			t.Fatalf("server identity: %+v: %v", gotServer, err)
		}
		actualServer := *gotServer.Server
		actualServer.Packages = append([]upstreammodel.Package(nil), actualServer.Packages...)
		for i := range actualServer.Packages {
			p := &actualServer.Packages[i]
			if len(p.Transport.Headers) == 0 {
				p.Transport.Headers = nil
			}
			if len(p.RuntimeArguments) == 0 {
				p.RuntimeArguments = nil
			}
			if len(p.PackageArguments) == 0 {
				p.PackageArguments = nil
			}
			if len(p.EnvironmentVariables) == 0 {
				p.EnvironmentVariables = nil
			}
		}
		if !reflect.DeepEqual(actualServer, server) {
			t.Fatalf("server replacement: got=%#v, want=%#v", actualServer, server)
		}
		gotSkill, err := entries.GetEntry(ctx, source.ID, persistence.SkillKind, skill.Name, persistence.VersionSelector{Exact: "1"})
		if err != nil || gotSkill.ID != ids[persistence.SkillKind] {
			t.Fatalf("skill identity: %+v %v", gotSkill, err)
		}
		gotPlugin, err := entries.GetEntry(ctx, source.ID, persistence.PluginKind, plugin.Name, persistence.VersionSelector{Exact: "1"})
		if err != nil || gotPlugin.ID != ids[persistence.PluginKind] {
			t.Fatalf("plugin identity: %+v %v", gotPlugin, err)
		}
		skill.Status, plugin.Status = testStatusActive, testStatusActive
		if !reflect.DeepEqual(formats.SkillPayload(gotSkill.Skill), skill) || !reflect.DeepEqual(formats.PluginPayload(gotPlugin.Plugin), plugin) {
			t.Fatalf("replacement payloads: skill=%+v want=%+v plugin=%+v want=%+v", formats.SkillPayload(gotSkill.Skill), skill, formats.PluginPayload(gotPlugin.Plugin), plugin)
		}
	})
	t.Run("long filter cursor", func(t *testing.T) {
		t.Parallel()
		sources, entries := factory(t)
		ctx := t.Context()
		source := newEntrySource(t, sources, "long-filter")
		reg := model.Snapshot{}
		reg.Data.Skills = []thvregistry.Skill{conformanceSkill("a"), conformanceSkill("b")}
		if err := entries.ReplaceSnapshot(ctx, source, reg); err != nil {
			t.Fatal(err)
		}
		for _, search := range []string{strings.Repeat("<", 80), strings.Repeat("界", 80)} {
			reg.Data.Skills[0].Description = search
			reg.Data.Skills[1].Description = search
			if err := entries.ReplaceSnapshot(ctx, source, reg); err != nil {
				t.Fatal(err)
			}
			opts := persistence.ListOptions{SourceID: source.ID, Kind: persistence.SkillKind, Limit: 1, Search: search}
			page, err := entries.ListEntries(ctx, opts)
			if err != nil || len(page.Entries) != 1 || page.NextCursor == "" {
				t.Fatalf("first page: %+v %v", page, err)
			}
			opts.Cursor = page.NextCursor
			page, err = entries.ListEntries(ctx, opts)
			if err != nil || len(page.Entries) != 1 || page.NextCursor != "" {
				t.Fatalf("continuation: %+v %v", page, err)
			}
			opts.Search += "!"
			if _, err = entries.ListEntries(ctx, opts); !errors.Is(err, persistence.ErrInvalid) {
				t.Fatalf("mismatched filter: %v", err)
			}
		}
		// Raw long filters are accepted even when they have no matches.
		for _, search := range []string{strings.Repeat("<", 500), strings.Repeat("界", 1500), strings.Repeat("x", 5000)} {
			page, err := entries.ListEntries(ctx, persistence.ListOptions{SourceID: source.ID, Kind: persistence.SkillKind, Limit: 1, Search: search})
			if err != nil || len(page.Entries) != 0 || page.NextCursor != "" {
				t.Fatalf("long search: %+v %v", page, err)
			}
		}
	})
	t.Run("snapshot and source ownership", func(t *testing.T) {
		t.Parallel()
		sources, entries := factory(t)
		ctx := t.Context()
		a := newEntrySource(t, sources, "alpha")
		b := newEntrySource(t, sources, "beta")
		reg := model.Snapshot{}
		for _, version := range []string{testCustomVersion, testVersion, testSemverTie} {
			reg.Data.Servers = append(reg.Data.Servers, conformanceServer(version))
			reg.Data.Skills = append(reg.Data.Skills, conformanceSkill(version))
			reg.Data.Plugins = append(reg.Data.Plugins, conformancePlugin(version))
		}
		if err := entries.ReplaceSnapshot(ctx, a, reg); err != nil {
			t.Fatal(err)
		}
		other := model.Snapshot{}
		other.Data.Skills = []thvregistry.Skill{conformanceSkill("other")}
		if err := entries.ReplaceSnapshot(ctx, b, other); err != nil {
			t.Fatal(err)
		}
		for _, kind := range []persistence.EntryKind{persistence.ServerKind, persistence.SkillKind, persistence.PluginKind} {
			name := testSkillName
			if kind == persistence.ServerKind {
				name = testServerName
			}
			got, err := entries.GetEntry(ctx, a.ID, kind, name, persistence.VersionSelector{Latest: true})
			if err != nil || got.Version != testSemverTie || !got.IsLatest || got.SourceID != a.ID || got.ID == "" {
				t.Fatalf("%s latest=%+v err=%v", kind, got, err)
			}
			if kind == persistence.SkillKind && (got.Skill == nil || got.Skill.ID != got.ID) {
				t.Fatalf("skill ID mismatch: %+v", got)
			}
			if kind == persistence.PluginKind && (got.Plugin == nil || got.Plugin.ID != got.ID) {
				t.Fatalf("plugin ID mismatch: %+v", got)
			}
			for _, version := range []string{testCustomVersion, testVersion, testSemverTie} {
				v, e := entries.GetEntry(ctx, a.ID, kind, name, persistence.VersionSelector{Exact: version})
				if e != nil || v.Version != version || v.ID == "" {
					t.Fatalf("%s %s = %+v %v", kind, version, v, e)
				}
			}
		}
		page, err := entries.ListEntries(ctx, persistence.ListOptions{SourceID: a.ID, Kind: persistence.SkillKind, Limit: 2})
		if err != nil || len(page.Entries) != 2 || page.NextCursor == "" || page.Entries[0].Version != testVersion {
			t.Fatalf("page=%+v err=%v", page, err)
		}
		next, err := entries.ListEntries(ctx, persistence.ListOptions{SourceID: a.ID, Kind: persistence.SkillKind, Limit: 2, Cursor: page.NextCursor})
		if err != nil || len(next.Entries) != 1 || next.Entries[0].Version != testSemverTie || next.NextCursor != "" {
			t.Fatalf("next=%+v err=%v", next, err)
		}
		if _, err := entries.ListEntries(ctx, persistence.ListOptions{SourceID: b.ID, Kind: persistence.SkillKind, Limit: 2, Cursor: page.NextCursor}); !errors.Is(err, persistence.ErrInvalid) {
			t.Fatalf("cross-source cursor: %v", err)
		}
		if _, err := entries.ListEntries(ctx, persistence.ListOptions{SourceID: a.ID, Kind: persistence.PluginKind, Limit: 2, Cursor: page.NextCursor}); !errors.Is(err, persistence.ErrInvalid) {
			t.Fatalf("cross-kind cursor: %v", err)
		}
		if _, err := entries.ListEntries(ctx, persistence.ListOptions{SourceID: a.ID, Kind: persistence.SkillKind, Limit: 0}); !errors.Is(err, persistence.ErrInvalid) {
			t.Fatalf("invalid limit: %v", err)
		}
		latest, err := entries.ListEntries(ctx, persistence.ListOptions{SourceID: a.ID, Kind: persistence.SkillKind, Limit: 10, LatestOnly: true})
		if err != nil || len(latest.Entries) != 1 || latest.Entries[0].Version != testSemverTie {
			t.Fatalf("latest page=%+v %v", latest, err)
		}
		match, err := entries.ListEntries(ctx, persistence.ListOptions{SourceID: a.ID, Kind: persistence.SkillKind, Limit: 10, Search: testSkillDescription})
		if err != nil || len(match.Entries) != 3 {
			t.Fatalf("search=%+v %v", match, err)
		}
		before, err := entries.GetEntry(ctx, a.ID, persistence.SkillKind, testSkillName, persistence.VersionSelector{Exact: testCustomVersion})
		if err != nil {
			t.Fatal(err)
		}
		if err := entries.ReplaceSnapshot(ctx, a, model.Snapshot{}); err != nil {
			t.Fatal(err)
		}
		if _, err := entries.GetEntry(ctx, a.ID, persistence.SkillKind, testSkillName, persistence.VersionSelector{Exact: testCustomVersion}); !errors.Is(err, persistence.ErrNotFound) {
			t.Fatal(err)
		}
		otherEntry, err := entries.GetEntry(ctx, b.ID, persistence.SkillKind, testSkillName, persistence.VersionSelector{Exact: "other"})
		if err != nil || otherEntry.SourceID != b.ID {
			t.Fatalf("other=%+v %v", otherEntry, err)
		}
		if err := entries.ReplaceSnapshot(ctx, a, reg); err != nil {
			t.Fatal(err)
		}
		unchanged, err := entries.GetEntry(ctx, a.ID, persistence.SkillKind, testSkillName, persistence.VersionSelector{Exact: testCustomVersion})
		if err != nil || unchanged.ID == "" || unchanged.ID == before.ID {
			t.Fatalf("recreated version: %+v / %+v / %v", unchanged, before, err)
		}
		if err := entries.ReplaceSnapshot(ctx, a, reg); err != nil {
			t.Fatal(err)
		}
		same, err := entries.GetEntry(ctx, a.ID, persistence.SkillKind, testSkillName, persistence.VersionSelector{Exact: testCustomVersion})
		if err != nil || same.ID != unchanged.ID {
			t.Fatalf("unchanged ID: %+v / %+v / %v", same, unchanged, err)
		}
		dup := model.Snapshot{}
		dup.Data.Skills = []thvregistry.Skill{conformanceSkill("1"), conformanceSkill("1")}
		if err := entries.ReplaceSnapshot(ctx, a, dup); !errors.Is(err, persistence.ErrInvalid) {
			t.Fatalf("duplicate: %v", err)
		}
		if got, e := entries.GetEntry(ctx, a.ID, persistence.SkillKind, testSkillName, persistence.VersionSelector{Latest: true}); e != nil || got.Version != testSemverTie {
			t.Fatalf("rollback duplicate: %+v %v", got, e)
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if err := entries.ReplaceSnapshot(canceled, a, model.Snapshot{}); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled: %v", err)
		}
		if got, e := entries.GetEntry(ctx, a.ID, persistence.SkillKind, testSkillName, persistence.VersionSelector{Latest: true}); e != nil || got.Version != testSemverTie {
			t.Fatalf("rollback canceled: %+v %v", got, e)
		}
		if err := sources.DeleteSource(ctx, a.Name); err != nil {
			t.Fatal(err)
		}
		replacement := newEntrySource(t, sources, "alpha")
		if err := entries.ReplaceSnapshot(ctx, a, reg); !errors.Is(err, persistence.ErrNotFound) && !errors.Is(err, persistence.ErrConflict) {
			t.Fatalf("stale source: %v", err)
		}
		if _, err := entries.GetEntry(ctx, replacement.ID, persistence.SkillKind, testSkillName, persistence.VersionSelector{Latest: true}); !errors.Is(err, persistence.ErrNotFound) {
			t.Fatal(err)
		}
	})
	t.Run("total latest order across input permutations", func(t *testing.T) {
		t.Parallel()
		sources, entries := factory(t)
		ctx := t.Context()
		source := newEntrySource(t, sources, "versions")
		for _, versions := range [][]string{{testCustomVersion, testVersion, testSemverTie}, {testSemverTie, testVersion, testCustomVersion}, {"3.0.0-beta.1", "3.0.0", "0.9"}} {
			snapshot := model.Snapshot{}
			for _, version := range versions {
				snapshot.Data.Servers = append(snapshot.Data.Servers, conformanceServer(version))
				snapshot.Data.Skills = append(snapshot.Data.Skills, conformanceSkill(version))
				snapshot.Data.Plugins = append(snapshot.Data.Plugins, conformancePlugin(version))
			}
			if err := entries.ReplaceSnapshot(ctx, source, snapshot); err != nil {
				t.Fatal(err)
			}
			want := testSemverTie
			if versions[0] == "3.0.0-beta.1" {
				want = "3.0.0"
			}
			for _, item := range []struct {
				kind persistence.EntryKind
				name string
			}{{persistence.ServerKind, testServerName}, {persistence.SkillKind, testSkillName}, {persistence.PluginKind, testSkillName}} {
				got, err := entries.GetEntry(ctx, source.ID, item.kind, item.name, persistence.VersionSelector{Latest: true})
				if err != nil || got.Version != want {
					t.Fatalf("%s latest in %v: %+v / %v", item.kind, versions, got, err)
				}
			}
		}
	})
	t.Run("source list filters and paging", func(t *testing.T) {
		t.Parallel()
		sources, entries := factory(t)
		ctx := t.Context()
		source := newEntrySource(t, sources, "filters")
		snapshot := model.Snapshot{}
		a := conformanceSkill("a")
		b := conformanceSkill("b")
		a.Title = testMatchingTitle
		b.Title = testMatchingTitle
		z := conformanceSkill("a")
		z.Name = "z-other"
		z.Title = testMatchingTitle
		snapshot.Data.Skills = []thvregistry.Skill{b, z, a}
		if err := entries.ReplaceSnapshot(ctx, source, snapshot); err != nil {
			t.Fatal(err)
		}
		page, err := entries.ListEntries(ctx, persistence.ListOptions{SourceID: source.ID, Kind: persistence.SkillKind, Limit: 1, Search: testSearchTerm})
		if err != nil || len(page.Entries) != 1 || page.Entries[0].Name != testSkillName || page.Entries[0].Version != "a" || page.NextCursor == "" || len(page.NextCursor) > persistence.MaxCursorBytes {
			t.Fatalf("first page: %+v / %v", page, err)
		}
		for _, cursor := range []string{"!", strings.Repeat("a", persistence.MaxCursorBytes+1)} {
			if _, err = entries.ListEntries(ctx, persistence.ListOptions{SourceID: source.ID, Kind: persistence.SkillKind, Limit: 1, Search: testSearchTerm, Cursor: cursor}); !errors.Is(err, persistence.ErrInvalid) {
				t.Fatalf("invalid cursor %q: %v", cursor, err)
			}
		}
		if _, err = entries.ListEntries(ctx, persistence.ListOptions{SourceID: source.ID, Kind: persistence.SkillKind, Limit: 1, Search: "changed", Cursor: page.NextCursor}); !errors.Is(err, persistence.ErrInvalid) {
			t.Fatalf("filter-bound cursor: %v", err)
		}
		page, err = entries.ListEntries(ctx, persistence.ListOptions{SourceID: source.ID, Kind: persistence.SkillKind, Limit: 1, Search: testSearchTerm, Cursor: page.NextCursor})
		if err != nil || len(page.Entries) != 1 || page.Entries[0].Version != "b" || page.NextCursor == "" {
			t.Fatalf("second page: %+v / %v", page, err)
		}
		page, err = entries.ListEntries(ctx, persistence.ListOptions{SourceID: source.ID, Kind: persistence.SkillKind, Limit: 1, Search: testSearchTerm, Cursor: page.NextCursor})
		if err != nil || len(page.Entries) != 1 || page.Entries[0].Name != "z-other" || page.NextCursor != "" {
			t.Fatalf("last page: %+v / %v", page, err)
		}
		only, err := entries.ListEntries(ctx, persistence.ListOptions{SourceID: source.ID, Kind: persistence.SkillKind, Limit: 10, Name: testSkillName, LatestOnly: true})
		if err != nil || len(only.Entries) != 1 || only.Entries[0].Version != "b" {
			t.Fatalf("name + latest: %+v / %v", only, err)
		}
		literal, err := entries.ListEntries(ctx, persistence.ListOptions{SourceID: source.ID, Kind: persistence.SkillKind, Limit: 10, Search: "%"})
		if err != nil || len(literal.Entries) != 0 {
			t.Fatalf("literal search: %+v / %v", literal, err)
		}
	})
	t.Run("managed versions", func(t *testing.T) {
		t.Parallel()
		sources, entries := factory(t)
		ctx := t.Context()
		external := newEntrySource(t, sources, "external")
		managed, e := sources.CreateSource(ctx, persistence.SourceDefinition{Name: testManagedKind, Managed: &persistence.ManagedSpec{}})
		if e != nil {
			t.Fatal(e)
		}
		for _, tc := range []struct {
			kind    persistence.EntryKind
			name    string
			publish func(string) persistence.Entry
			entry   func(persistence.Entry) (string, string, map[string]any)
		}{
			{kind: persistence.SkillKind, name: testDefaultSkillName, publish: func(status string) persistence.Entry {
				return persistence.Entry{Kind: persistence.SkillKind, Name: testDefaultSkillName, Version: "1", Skill: &model.Skill{Namespace: testNamespace, Name: testDefaultSkillName, Version: "1", Description: testSkillDescription, Status: status, Metadata: map[string]any{testMetadataKey: testMetadataValue}}}
			}, entry: func(v persistence.Entry) (string, string, map[string]any) {
				return v.Skill.Status, v.Skill.ID, v.Skill.Metadata
			}},
			{kind: persistence.PluginKind, name: testDefaultPluginName, publish: func(status string) persistence.Entry {
				return persistence.Entry{Kind: persistence.PluginKind, Name: testDefaultPluginName, Version: "1", Plugin: &model.Plugin{Namespace: testNamespace, Name: testDefaultPluginName, Version: "1", Description: testPluginDescription, Status: status, Metadata: map[string]any{testMetadataKey: testMetadataValue}}}
			}, entry: func(v persistence.Entry) (string, string, map[string]any) {
				return v.Plugin.Status, v.Plugin.ID, v.Plugin.Metadata
			}},
		} {
			input := tc.publish("")
			published, err := entries.Publish(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			status, payloadID, metadata := tc.entry(published)
			if status != testStatusActive || published.ID == "" || payloadID != published.ID || !published.IsLatest || !reflect.DeepEqual(metadata, map[string]any{testMetadataKey: testMetadataValue}) {
				t.Fatalf("default published %s: %+v", tc.kind, published)
			}
			if gotStatus, _, _ := tc.entry(input); gotStatus != "" {
				t.Fatalf("mutated %s input status: %q", tc.kind, gotStatus)
			}
			got, err := entries.GetEntry(ctx, managed.ID, tc.kind, tc.name, persistence.VersionSelector{Latest: true})
			if err != nil || got.ID != published.ID || !got.IsLatest {
				t.Fatalf("default %s get: %+v / %v", tc.kind, got, err)
			}
			status, payloadID, metadata = tc.entry(got)
			if status != testStatusActive || payloadID != got.ID || !reflect.DeepEqual(metadata, map[string]any{testMetadataKey: testMetadataValue}) {
				t.Fatalf("default %s get payload: %+v", tc.kind, got)
			}
			page, err := entries.ListEntries(ctx, persistence.ListOptions{SourceID: managed.ID, Kind: tc.kind, Name: tc.name, Limit: 1})
			if err != nil || len(page.Entries) != 1 || page.Entries[0].ID != published.ID || page.NextCursor != "" {
				t.Fatalf("default %s list: %+v / %v", tc.kind, page, err)
			}
			status, payloadID, metadata = tc.entry(page.Entries[0])
			if status != testStatusActive || payloadID != page.Entries[0].ID || !reflect.DeepEqual(metadata, map[string]any{testMetadataKey: testMetadataValue}) {
				t.Fatalf("default %s list payload: %+v", tc.kind, page.Entries[0])
			}
			explicit := tc.publish(testStatusDeprecated)
			explicit.Version = "2"
			if explicit.Skill != nil {
				explicit.Skill.Version = "2"
			}
			if explicit.Plugin != nil {
				explicit.Plugin.Version = "2"
			}
			published, err = entries.Publish(ctx, explicit)
			if err != nil {
				t.Fatal(err)
			}
			status, _, _ = tc.entry(published)
			if status != testStatusDeprecated {
				t.Fatalf("explicit %s status: %+v", tc.kind, published)
			}
		}
		reg := model.Snapshot{}
		reg.Data.Skills = []thvregistry.Skill{conformanceSkill(testVersion)}
		if e = entries.ReplaceSnapshot(ctx, external, reg); e != nil {
			t.Fatal(e)
		}
		if e = entries.ReplaceSnapshot(ctx, managed, reg); !errors.Is(e, persistence.ErrConflict) {
			t.Fatalf("managed snapshot: %v", e)
		}
		for _, version := range []string{testLiteralLatest, testVersion, "2.0.0"} {
			v := conformanceSkill(version)
			_, e = entries.Publish(ctx, persistence.Entry{Kind: persistence.SkillKind, Name: testSkillName, Version: version, Skill: &model.Skill{
				Namespace: v.Namespace, Name: v.Name, Version: v.Version, Description: v.Description, Metadata: map[string]any{"nested": map[string]any{testMetadataKey: testMetadataValue}},
			}})
			if e != nil {
				t.Fatal(e)
			}
		}
		if _, e = entries.Publish(ctx, persistence.Entry{Kind: persistence.SkillKind, Name: testSkillName, Version: testVersion, Skill: &model.Skill{Namespace: testNamespace, Name: testSkillName, Version: testVersion, Description: testSkillDescription}}); !errors.Is(e, persistence.ErrConflict) {
			t.Fatalf("publish conflict: %v", e)
		}
		literal, e := entries.GetEntry(ctx, managed.ID, persistence.SkillKind, testSkillName, persistence.VersionSelector{Exact: testLiteralLatest})
		if e != nil || literal.Version != testLiteralLatest {
			t.Fatalf("literal=%+v %v", literal, e)
		}
		latest, e := entries.GetEntry(ctx, managed.ID, persistence.SkillKind, testSkillName, persistence.VersionSelector{Latest: true})
		if e != nil || latest.Version != "2.0.0" {
			t.Fatalf("latest=%+v %v", latest, e)
		}
		nested := latest.Skill.Metadata["nested"].(map[string]any)
		nested[testMetadataKey] = "mutated"
		same, e := entries.GetEntry(ctx, managed.ID, persistence.SkillKind, testSkillName, persistence.VersionSelector{Latest: true})
		if e != nil || !reflect.DeepEqual(same.Skill.Metadata, map[string]any{"nested": map[string]any{testMetadataKey: testMetadataValue}}) {
			t.Fatalf("aliased metadata: %+v %v", same, e)
		}
		if e = entries.DeleteManaged(ctx, persistence.SkillKind, testSkillName, "2.0.0"); e != nil {
			t.Fatal(e)
		}
		next, e := entries.GetEntry(ctx, managed.ID, persistence.SkillKind, testSkillName, persistence.VersionSelector{Latest: true})
		if e != nil || next.Version != testVersion {
			t.Fatalf("recomputed latest=%+v %v", next, e)
		}
		if e = entries.DeleteManaged(ctx, persistence.SkillKind, testSkillName, "2.0.0"); !errors.Is(e, persistence.ErrNotFound) {
			t.Fatal(e)
		}
		fromExternal, e := entries.GetEntry(ctx, external.ID, persistence.SkillKind, testSkillName, persistence.VersionSelector{Exact: testVersion})
		if e != nil || fromExternal.Skill == nil {
			t.Fatalf("external untouched: %+v %v", fromExternal, e)
		}
		server := conformanceServer(testVersion)
		if _, e = entries.Publish(ctx, persistence.Entry{Kind: persistence.ServerKind, Name: server.Name, Version: server.Version, Server: &server}); e != nil {
			t.Fatalf("publish server: %v", e)
		}
		plugin := &model.Plugin{Namespace: testNamespace, Name: testSkillName, Version: testVersion, Description: testPluginDescription, Metadata: map[string]any{testMetadataKey: testMetadataValue}}
		if _, e = entries.Publish(ctx, persistence.Entry{Kind: persistence.PluginKind, Name: plugin.Name, Version: plugin.Version, Plugin: plugin}); e != nil {
			t.Fatalf("publish plugin: %v", e)
		}
		if _, e = entries.Publish(ctx, persistence.Entry{Kind: persistence.PluginKind, Name: plugin.Name, Version: plugin.Version, Plugin: plugin}); !errors.Is(e, persistence.ErrConflict) {
			t.Fatalf("plugin conflict: %v", e)
		}
		plugin.Metadata[testMetadataKey] = "caller mutation"
		published, e := entries.GetEntry(ctx, managed.ID, persistence.PluginKind, plugin.Name, persistence.VersionSelector{Exact: plugin.Version})
		if e != nil || published.Plugin == nil || published.Plugin.Metadata[testMetadataKey] != testMetadataValue {
			t.Fatalf("caller payload alias: %+v / %v", published, e)
		}
		if e = entries.DeleteManaged(ctx, persistence.SkillKind, testSkillName, testVersion); e != nil {
			t.Fatal(e)
		}
		for _, item := range []struct {
			kind persistence.EntryKind
			name string
		}{{persistence.ServerKind, server.Name}, {persistence.PluginKind, plugin.Name}} {
			got, err := entries.GetEntry(ctx, managed.ID, item.kind, item.name, persistence.VersionSelector{Latest: true})
			if err != nil || got.Version != testVersion {
				t.Fatalf("%s after skill delete: %+v / %v", item.kind, got, err)
			}
		}
		if e = entries.DeleteManaged(ctx, persistence.PluginKind, plugin.Name, plugin.Version); e != nil {
			t.Fatal(e)
		}
		if _, e = entries.GetEntry(ctx, managed.ID, persistence.ServerKind, server.Name, persistence.VersionSelector{Latest: true}); e != nil {
			t.Fatal(e)
		}
	})
}

// RunEntriesMulti checks that independent handles serialize shared-source writes.
func RunEntriesMulti(t *testing.T, factory EntriesPairFactory) {
	t.Helper()
	t.Run("concurrent handles serialize", func(t *testing.T) {
		t.Parallel()
		sources, a, b := factory(t)
		ctx := t.Context()
		source := newEntrySource(t, sources, "shared")
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for _, store := range []persistence.Entries{a, b} {
			wg.Add(1)
			go func(store persistence.Entries) {
				defer wg.Done()
				r := model.Snapshot{}
				r.Data.Skills = []thvregistry.Skill{conformanceSkill("1")}
				errs <- store.ReplaceSnapshot(ctx, source, r)
			}(store)
		}
		wg.Wait()
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
		got, e := a.GetEntry(ctx, source.ID, persistence.SkillKind, testSkillName, persistence.VersionSelector{Latest: true})
		if e != nil || got.Version != "1" {
			t.Fatalf("after concurrent snapshots: %+v %v", got, e)
		}
	})
}

func newEntrySource(t *testing.T, sources persistence.Sources, name string) persistence.SourceDefinition {
	t.Helper()
	s, e := sources.CreateSource(t.Context(), persistence.SourceDefinition{
		Name: name, API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h",
	})
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func conformanceServer(version string) upstream.ServerJSON {
	return upstream.ServerJSON{
		Schema: "https://example.org/server.schema.json", Name: testServerName, Version: version, Description: "server description",
	}
}
func conformanceSkill(version string) thvregistry.Skill {
	return thvregistry.Skill{Namespace: testNamespace, Name: testSkillName, Version: version, Description: testSkillDescription}
}
func conformancePlugin(version string) thvregistry.Plugin {
	return thvregistry.Plugin{Namespace: testNamespace, Name: testSkillName, Version: version, Description: testPluginDescription}
}
