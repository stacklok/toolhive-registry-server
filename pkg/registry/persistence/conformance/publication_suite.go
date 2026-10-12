package conformance

import (
	"encoding/json"
	"errors"
	"testing"

	upstreammodel "github.com/modelcontextprotocol/registry/pkg/model"
	thvregistry "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/formats"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

const (
	publicationStdio            = "stdio"
	publicationExtension        = "extension"
	publicationPluginRepository = "https://example.org/plugin.git"
)

// RunPublication exercises the public application with a replacement backend,
// including authoritative missing-singleton classification and exact deletion.
//
//nolint:gocyclo // Each kind checks the complete publication/deletion transition sequence.
func RunPublication(t *testing.T, factory EntriesFactory) {
	t.Helper()
	for _, kind := range []persistence.EntryKind{persistence.ServerKind, persistence.SkillKind, persistence.PluginKind} {
		t.Run("publication/"+string(kind), func(t *testing.T) {
			t.Parallel()
			sources, entries := factory(t)
			publisher, err := catalog.NewPublisher(entries)
			if err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			for _, invalid := range []persistence.Entry{
				{Kind: kind, Name: testSkillName, Version: "1"},
				{Kind: kind, Name: testSkillName, Version: "1", Server: &model.Server{}, Skill: &model.Skill{}, Plugin: &model.Plugin{}},
			} {
				if _, e := entries.Publish(ctx, invalid); !errors.Is(e, persistence.ErrInvalid) {
					t.Fatalf("backend accepted invalid publication union: %v", e)
				}
			}
			input := publicationEntry(kind, "1")
			_, err = publisher.Publish(ctx, input)
			if !errors.Is(err, persistence.ErrNoManagedSource) || !errors.Is(err, persistence.ErrNotFound) {
				t.Fatalf("missing singleton: %v", err)
			}
			if err = publisher.DeleteManaged(ctx, kind, input.Name, input.Version); !errors.Is(err, persistence.ErrNoManagedSource) {
				t.Fatalf("delete without singleton: %v", err)
			}
			managed, err := sources.CreateSource(ctx, persistence.SourceDefinition{Name: "managed", Managed: &persistence.ManagedSpec{}})
			if err != nil {
				t.Fatal(err)
			}
			for _, version := range []string{testLiteralLatest, "1", "2"} {
				input = publicationEntry(kind, version)
				stored, e := publisher.Publish(ctx, input)
				if e != nil || stored.ID == "" || stored.SourceID != managed.ID || stored.Kind != kind || stored.Version != version {
					t.Fatalf("publication result: %+v / %v", stored, e)
				}
				if string(publicationPayload(t, input)) != string(publicationPayload(t, stored)) {
					t.Fatalf("payload fidelity: input %s stored %s", publicationPayload(t, input), publicationPayload(t, stored))
				}
				_, e = publisher.Publish(ctx, input)
				if !errors.Is(e, persistence.ErrConflict) || errors.Is(e, persistence.ErrAlreadyExists) {
					t.Fatalf("exact duplicate classification: %v", e)
				}
			}
			// Preserve unrelated managed history and another source's history.
			other := publicationEntry(persistence.SkillKind, "1")
			other.Name, other.Skill.Name = "unrelated", "unrelated"
			if _, err = publisher.Publish(ctx, other); err != nil {
				t.Fatal(err)
			}
			external := newEntrySource(t, sources, "external")
			snapshot := model.Snapshot{}
			snapshot.Data.Servers = []model.Server{conformanceServer("1")}
			if err = entries.ReplaceSnapshot(ctx, external, snapshot); err != nil {
				t.Fatal(err)
			}
			if err = publisher.DeleteManaged(ctx, kind, input.Name, testLiteralLatest); err != nil {
				t.Fatal(err)
			}
			_, err = entries.GetEntry(ctx, managed.ID, kind, input.Name, persistence.VersionSelector{Exact: testLiteralLatest})
			if !errors.Is(err, persistence.ErrNotFound) {
				t.Fatal("literal latest not removed", err)
			}
			latest, err := entries.GetEntry(ctx, managed.ID, kind, input.Name, persistence.VersionSelector{Latest: true})
			if err != nil || latest.Version != "2" {
				t.Fatalf("literal deletion removed selector latest: %+v / %v", latest, err)
			}
			if err = publisher.DeleteManaged(ctx, kind, input.Name, "2"); err != nil {
				t.Fatal(err)
			}
			latest, err = entries.GetEntry(ctx, managed.ID, kind, input.Name, persistence.VersionSelector{Latest: true})
			if err != nil || latest.Version != "1" || !latest.IsLatest {
				t.Fatalf("latest not recomputed: %+v / %v", latest, err)
			}
			if string(publicationPayload(t, latest)) != string(publicationPayload(t, publicationEntry(kind, "1"))) {
				t.Fatal("surviving version related metadata changed after deletion")
			}
			err = publisher.DeleteManaged(ctx, kind, input.Name, "2")
			if !errors.Is(err, persistence.ErrNotFound) || errors.Is(err, persistence.ErrNoManagedSource) {
				t.Fatalf("missing version conflated with missing singleton: %v", err)
			}
			if _, err = entries.GetEntry(ctx, managed.ID, other.Kind, other.Name, persistence.VersionSelector{Exact: "1"}); err != nil {
				t.Fatal("unrelated managed history changed", err)
			}
			_, err = entries.GetEntry(ctx, external.ID, persistence.ServerKind, testServerName, persistence.VersionSelector{Exact: "1"})
			if err != nil {
				t.Fatal("other source changed", err)
			}
		})
	}
}

func publicationEntry(kind persistence.EntryKind, version string) persistence.Entry {
	entry := persistence.Entry{Kind: kind, Version: version}
	switch kind {
	case persistence.ServerKind:
		server := conformanceServer(version)
		server.Icons = []upstreammodel.Icon{{Src: "https://example.org/icon.png", Sizes: []string{"32x32"}}}
		server.Remotes = []upstreammodel.Transport{{Type: "streamable-http", URL: "https://example.org/mcp",
			Variables: map[string]upstreammodel.Input{"region": {Description: "region"}}}}
		server.Packages = []upstreammodel.Package{{RegistryType: "npm", Identifier: "package", Version: "1.0.0",
			Transport: upstreammodel.Transport{Type: publicationStdio}}}
		entry.Name, entry.Server = server.Name, &server
	case persistence.SkillKind:
		entry.Name = testSkillName
		entry.Skill = &model.Skill{Namespace: testNamespace, Name: entry.Name, Version: version, Description: testSkillDescription,
			Status: testStatusActive, Metadata: map[string]any{testMetadataKey: map[string]any{testMetadataKey: testMetadataValue}},
			AllowedTools: []string{"tool"}, Compatibility: "compatibility", License: "MIT",
			Repository: &model.SkillRepository{URL: "https://example.org/skill.git"},
			Packages:   []model.SkillPackage{{RegistryType: model.PackageFormatOCI, Identifier: "example/skill:1"}},
			Provenance: &thvregistry.Provenance{RepositoryURI: "https://example.org/source"},
			Icons:      []model.SkillIcon{{Src: "https://example.org/skill.png"}}, Meta: map[string]any{publicationExtension: true}}
	case persistence.PluginKind:
		entry.Name = testSkillName
		entry.Plugin = &model.Plugin{Namespace: testNamespace, Name: entry.Name, Version: version, Description: testPluginDescription,
			Status: testStatusActive, Metadata: map[string]any{testMetadataKey: testMetadataValue}, License: "MIT",
			Repository: &model.PluginRepository{URL: publicationPluginRepository},
			Packages:   []model.PluginPackage{{RegistryType: model.PackageFormatGit, URL: publicationPluginRepository, Commit: "abc"}},
			Icons:      []model.PluginIcon{{Src: "https://example.org/plugin.png"}}, Meta: map[string]any{publicationExtension: true}}
	}
	return entry
}

func publicationPayload(t *testing.T, entry persistence.Entry) []byte {
	t.Helper()
	var payload any
	switch entry.Kind {
	case persistence.ServerKind:
		payload = formats.ServerPayload(entry.Server)
	case persistence.SkillKind:
		payload = formats.SkillPayload(entry.Skill)
	case persistence.PluginKind:
		payload = formats.PluginPayload(entry.Plugin)
	}
	result, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
