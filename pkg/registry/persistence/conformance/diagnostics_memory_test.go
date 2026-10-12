package conformance

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

var _ persistence.EntryDiagnostics = (*memory)(nil)

func (m *memory) ListSourceVersions(ctx context.Context, name string) ([]persistence.DiagnosticVersion, error) {
	if name == "" {
		return nil, persistence.ErrInvalid
	}
	m.Lock()
	defer m.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	source, exists := m.sources[name]
	if !exists {
		return nil, persistence.ErrNotFound
	}
	result := []persistence.DiagnosticVersion{}
	for _, entry := range m.records {
		if entry.SourceID == source.ID {
			result = append(result, m.diagnostic(entry))
		}
	}
	sort.Slice(result, func(i, j int) bool { return diagnosticLess(result[i], result[j]) })
	return result, nil
}

func (m *memory) ListViewVersions(ctx context.Context, name string) ([]persistence.ViewDiagnosticVersion, error) {
	if name == "" {
		return nil, persistence.ErrInvalid
	}
	m.Lock()
	defer m.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	view, exists := m.views[name]
	if !exists {
		return nil, persistence.ErrNotFound
	}
	result := []persistence.ViewDiagnosticVersion{}
	for pos, sourceName := range view.Sources {
		source := m.sources[sourceName]
		position := int32(pos)
		if positions, ok := m.positions[name]; ok {
			if legacy, ok := positions[sourceName]; ok {
				position = legacy
			}
		}
		for _, entry := range m.records {
			if entry.SourceID == source.ID {
				result = append(result, persistence.ViewDiagnosticVersion{
					DiagnosticVersion: m.diagnostic(entry), SourceName: sourceName, SourceID: source.ID, Position: position,
				})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if a.Name == b.Name && a.Version == b.Version && a.Kind == b.Kind {
			if a.Position != b.Position {
				return a.Position < b.Position
			}
			return a.SourceName < b.SourceName
		}
		return diagnosticLess(a.DiagnosticVersion, b.DiagnosticVersion)
	})
	return result, nil
}

func (m *memory) tieViewPositions(ctx context.Context, name string) error {
	m.Lock()
	defer m.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	view, exists := m.views[name]
	if !exists {
		return persistence.ErrNotFound
	}
	m.positions[name] = make(map[string]int32, len(view.Sources))
	for _, sourceName := range view.Sources {
		m.positions[name][sourceName] = 0
	}
	return nil
}

func (m *memory) diagnostic(e persistence.Entry) persistence.DiagnosticVersion {
	v := persistence.DiagnosticVersion{Kind: e.Kind, Name: e.Name, Version: e.Version,
		CreatedAt: m.created[memKey(e)], UpdatedAt: m.updated[memKey(e)]}
	switch e.Kind {
	case persistence.ServerKind:
		v.Title, v.Description = e.Server.Title, e.Server.Description
	case persistence.SkillKind:
		v.Title, v.Description = e.Skill.Title, e.Skill.Description
	case persistence.PluginKind:
		v.Title, v.Description = e.Plugin.Title, e.Plugin.Description
	}
	return v
}

func diagnosticLess(a, b persistence.DiagnosticVersion) bool {
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	if a.Version != b.Version {
		return a.Version < b.Version
	}
	return a.Kind < b.Kind
}

func TestMemoryHistoricalDiagnosticKinds(t *testing.T) {
	t.Parallel()
	m := fresh()
	a := newEntrySource(t, m, "raw-a")
	b := newEntrySource(t, m, "raw-b")
	createView(t, m, "raw", []string{b.Name, a.Name})
	for _, source := range []persistence.SourceDefinition{a, b} {
		for _, kind := range []persistence.EntryKind{persistence.ServerKind, persistence.SkillKind, persistence.PluginKind} {
			for _, version := range []string{"a", "Z", "10", "2"} {
				e := persistence.Entry{Kind: kind, SourceID: source.ID, Name: "same", Version: version}
				switch kind {
				case persistence.ServerKind:
					e.Server = &model.Server{Title: "title", Description: "description"}
				case persistence.SkillKind:
					e.Skill = &model.Skill{Title: "title", Description: "description"}
				case persistence.PluginKind:
					e.Plugin = &model.Plugin{Title: "title", Description: "description"}
				}
				m.add(e)
				m.created[memKey(e)], m.updated[memKey(e)] = time.Time{}, time.Time{}
			}
		}
	}
	rows, err := m.ListSourceVersions(t.Context(), a.Name)
	if err != nil || len(rows) != 12 {
		t.Fatal(rows, err)
	}
	view, err := m.ListViewVersions(t.Context(), "raw")
	if err != nil || len(view) != 24 {
		t.Fatal(view, err)
	}
	for i, row := range rows {
		if !row.CreatedAt.IsZero() || !row.UpdatedAt.IsZero() || row.Title != "title" || row.Description != "description" ||
			row.Version != []string{"10", "2", "Z", "a"}[i/3] ||
			row.Kind != []persistence.EntryKind{persistence.ServerKind, persistence.PluginKind, persistence.SkillKind}[i%3] {
			t.Fatal("raw collision/timestamp ordering", row)
		}
		if view[2*i].DiagnosticVersion != row || view[2*i+1].DiagnosticVersion != row ||
			view[2*i].SourceID != b.ID || view[2*i+1].SourceID != a.ID {
			t.Fatal("raw duplicates lost", view)
		}
	}
}

func TestMemoryDiagnosticLegacyTiedPositions(t *testing.T) {
	t.Parallel()
	RunDiagnosticsTiedPositions(t, func(*testing.T) (persistence.Definitions, persistence.Entries, persistence.EntryDiagnostics, func(context.Context, string) error) {
		m := fresh()
		return m, m, m, m.tieViewPositions
	})
}

func TestMemoryDiagnostics(t *testing.T) {
	t.Parallel()
	RunDiagnostics(t, func(*testing.T) (persistence.Definitions, persistence.Entries, persistence.EntryDiagnostics) {
		m := fresh()
		return m, m, m
	})
}
