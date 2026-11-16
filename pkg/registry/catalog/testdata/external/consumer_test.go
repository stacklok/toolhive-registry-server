package consumer

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/formats"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// ownBackend contains application-owned values, with no registry database or
// standalone service. Its single-source view cannot have shadowed names.
type ownBackend struct{ entries []persistence.Entry }

func (b ownBackend) ListViewEntries(ctx context.Context, view string, q persistence.ViewQuery) (persistence.EntryPage, error) {
	if err := q.Validate(view); err != nil {
		return persistence.EntryPage{}, err
	}
	if err := ctx.Err(); err != nil {
		return persistence.EntryPage{}, err
	}
	if view != "catalog" {
		return persistence.EntryPage{}, persistence.ErrNotFound
	}
	page := persistence.EntryPage{Entries: []persistence.Entry{}}
	for _, entry := range b.entries {
		if entry.Kind != q.Kind || q.Name != "" && q.Name != entry.Name || q.VersionSelector.Exact != "" && q.VersionSelector.Exact != entry.Version || q.VersionSelector.Latest && !entry.IsLatest {
			continue
		}
		namespace := ""
		if entry.Skill != nil {
			namespace = entry.Skill.Namespace
		}
		if entry.Plugin != nil {
			namespace = entry.Plugin.Namespace
		}
		if q.Namespace != "" && namespace != q.Namespace {
			continue
		}
		// This small fixture has two versions per name; continuation is owned here.
		if q.Cursor != "" {
			if q.Cursor != "second-version" {
				return persistence.EntryPage{}, persistence.ErrInvalid
			}
			if entry.Version == "1" {
				continue
			}
		}
		if len(page.Entries) == q.Limit {
			page.NextCursor = "second-version"
			break
		}
		data, err := json.Marshal(entry)
		if err != nil {
			return persistence.EntryPage{}, err
		}
		var copied persistence.Entry
		if err = json.Unmarshal(data, &copied); err != nil {
			return persistence.EntryPage{}, err
		}
		page.Entries = append(page.Entries, copied)
	}
	return page, nil
}
func (b ownBackend) GetViewEntry(ctx context.Context, view string, name persistence.EntryName, selector persistence.VersionSelector) (persistence.Entry, error) {
	if err := name.Validate(view, selector); err != nil {
		return persistence.Entry{}, err
	}
	page, err := b.ListViewEntries(ctx, view, persistence.ViewQuery{Kind: name.Kind, Name: name.Name, Namespace: name.Namespace, VersionSelector: selector, Limit: 1})
	if err != nil {
		return persistence.Entry{}, err
	}
	if len(page.Entries) == 0 {
		return persistence.Entry{}, persistence.ErrNotFound
	}
	return page.Entries[0], nil
}

func TestIndependentBackendCatalogAndFormats(t *testing.T) {
	backend := ownBackend{}
	for _, kind := range []persistence.EntryKind{persistence.ServerKind, persistence.SkillKind, persistence.PluginKind} {
		for _, version := range []string{"1", "2"} {
			entry := persistence.Entry{Kind: kind, Name: "item", Version: version, IsLatest: version == "2", SourceID: "host-source"}
			switch kind {
			case persistence.ServerKind:
				entry.Name = "com.example/item"
				entry.Server = &model.Server{Name: entry.Name, Version: version, Description: "server"}
			case persistence.SkillKind:
				entry.Skill = &model.Skill{Name: entry.Name, Namespace: "com.example", Version: version, Description: "skill"}
			case persistence.PluginKind:
				entry.Plugin = &model.Plugin{Name: entry.Name, Namespace: "com.example", Version: version, Description: "plugin"}
			}
			backend.entries = append(backend.entries, entry)
		}
	}
	reader, err := catalog.NewReader(backend)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []persistence.EntryKind{persistence.ServerKind, persistence.SkillKind, persistence.PluginKind} {
		t.Run(string(kind), func(t *testing.T) {
			q := catalog.Query{Kind: kind, Limit: 1}
			page, err := reader.List(t.Context(), "catalog", q)
			if err != nil || len(page.Entries) != 1 || page.NextCursor != "second-version" {
				t.Fatalf("list: %+v / %v", page, err)
			}
			name := catalog.EntryName{Kind: kind, Name: page.Entries[0].Name}
			if kind != persistence.ServerKind {
				name.Namespace = "com.example"
			}
			exact, err := reader.Get(t.Context(), "catalog", name, catalog.VersionSelector{Exact: "1"})
			if err != nil || exact.Version != "1" {
				t.Fatalf("exact: %+v / %v", exact, err)
			}
			latest, err := reader.Get(t.Context(), "catalog", name, catalog.VersionSelector{Latest: true})
			if err != nil || latest.Version != "2" {
				t.Fatalf("latest: %+v / %v", latest, err)
			}
			history, err := reader.ListVersions(t.Context(), "catalog", name, catalog.PageRequest{Limit: 2})
			if err != nil || len(history.Entries) != 2 || history.NextCursor != "" {
				t.Fatalf("history: %+v / %v", history, err)
			}
			q.Cursor = page.NextCursor
			tail, err := reader.List(t.Context(), "catalog", q)
			if err != nil || len(tail.Entries) != 1 || tail.Entries[0].Version != "2" || tail.NextCursor != "" {
				t.Fatalf("tail: %+v / %v", tail, err)
			}
			var response any
			switch kind {
			case persistence.ServerKind:
				response = formats.ServerPage([]*model.Server{page.Entries[0].Server}, page.NextCursor)
			case persistence.SkillKind:
				response = formats.SkillPage([]*model.Skill{page.Entries[0].Skill}, page.NextCursor)
			case persistence.PluginKind:
				response = formats.PluginPage([]*model.Plugin{page.Entries[0].Plugin}, page.NextCursor)
			}
			data, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			var wire struct {
				Metadata struct {
					Count      int    `json:"count"`
					NextCursor string `json:"nextCursor"`
				} `json:"metadata"`
			}
			if err = json.Unmarshal(data, &wire); err != nil || wire.Metadata.Count != 1 || wire.Metadata.NextCursor != page.NextCursor {
				t.Fatalf("serialization: %s / %v", data, err)
			}
		})
	}
}
