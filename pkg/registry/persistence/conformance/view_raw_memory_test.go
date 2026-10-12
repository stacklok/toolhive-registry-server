package conformance

import (
	"errors"
	"strings"
	"testing"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

func TestMemoryRawViewKindsAndLongTokens(t *testing.T) {
	t.Parallel()
	m := fresh()
	a := newEntrySource(t, m, "raw-a")
	b := newEntrySource(t, m, "raw-b")
	createView(t, m, "raw", []string{a.Name, b.Name})
	long := strings.Repeat("x", 5000)
	// Seed historical raw identities directly, not through public payload validation.
	for _, kind := range []persistence.EntryKind{persistence.ServerKind, persistence.SkillKind, persistence.PluginKind} {
		for _, source := range []persistence.SourceDefinition{a, b} {
			if kind == persistence.PluginKind && source.ID == a.ID {
				continue
			}
			versions := []string{"2", "3"}
			if source.ID == b.ID {
				versions = []string{"1", "9"}
			}
			for _, version := range versions {
				v := persistence.Entry{Kind: kind, SourceID: source.ID, Name: "item", Version: version}
				switch kind {
				case persistence.ServerKind:
					v.Server = &model.Server{Name: v.Name, Version: version, Description: long}
				case persistence.SkillKind:
					v.Skill = &model.Skill{Name: v.Name, Namespace: "com.example", Version: version, Description: long}
				case persistence.PluginKind:
					v.Plugin = &model.Plugin{Name: v.Name, Namespace: "com.example", Version: version, Description: long}
				}
				m.add(v)
			}
		}
	}
	for _, kind := range []persistence.EntryKind{persistence.ServerKind, persistence.SkillKind, persistence.PluginKind} {
		winner, best := a.ID, "3"
		if kind == persistence.PluginKind {
			winner, best = b.ID, "9"
		}
		got, e := m.GetViewEntry(t.Context(), "raw", persistence.EntryName{Kind: kind, Name: "item"}, persistence.VersionSelector{Latest: true})
		if e != nil || got.SourceID != winner || got.Version != best {
			t.Fatalf("raw kind %s: %+v / %v", kind, got, e)
		}
		q := persistence.ViewQuery{Kind: kind, Search: long, Limit: 1}
		page, e := m.ListViewEntries(t.Context(), "raw", q)
		if e != nil || len(page.Entries) != 1 || page.Entries[0].SourceID != winner || page.NextCursor == "" || len(page.NextCursor) > 4096 {
			t.Fatalf("long filter: %+v / %v", page, e)
		}
		q.Cursor = page.NextCursor
		page, e = m.ListViewEntries(t.Context(), "raw", q)
		if e != nil || len(page.Entries) != 1 || page.NextCursor != "" {
			t.Fatalf("long filter tail: %+v / %v", page, e)
		}
	}
	m = fresh()
	a = newEntrySource(t, m, "huge")
	createView(t, m, "raw", []string{a.Name})
	for _, version := range []string{strings.Repeat("<", 800), strings.Repeat("<", 800) + "a"} {
		m.add(persistence.Entry{SourceID: a.ID, Kind: persistence.SkillKind, Name: "item", Version: version, Skill: &model.Skill{Name: "item", Namespace: "com.example", Version: version, Description: "legacy"}})
	}
	page, e := m.ListViewEntries(t.Context(), "raw", persistence.ViewQuery{Kind: persistence.SkillKind, Limit: 1})
	if !errors.Is(e, persistence.ErrInvalid) || page.NextCursor != "" || len(page.Entries) != 0 {
		t.Fatalf("huge identity truncated: %+v / %v", page, e)
	}
}
