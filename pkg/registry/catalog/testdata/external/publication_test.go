package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/formats"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// Independent, single-threaded fixture storage, deliberately not a production
// backend. It needs only public publication and diagnostic contracts.
type publicationBackend struct {
	managed bool
	entries map[[3]string]persistence.Entry
	times   map[[3]string]time.Time
}

func (b *publicationBackend) Publish(ctx context.Context, e persistence.Entry) (persistence.Entry, error) {
	if err := ctx.Err(); err != nil {
		return persistence.Entry{}, err
	}
	if err := e.Validate(); err != nil {
		return persistence.Entry{}, err
	}
	if !b.managed {
		return persistence.Entry{}, persistence.ErrNoManagedSource
	}
	key := [3]string{string(e.Kind), e.Name, e.Version}
	if _, exists := b.entries[key]; exists {
		return persistence.Entry{}, persistence.ErrConflict
	}
	e.ID, e.SourceID = e.Name+"@"+e.Version, "host-managed-incarnation"
	b.entries[key] = copyEntry(e)
	b.times[key] = time.Now().UTC()
	b.recompute(e.Kind, e.Name)
	return copyEntry(b.entries[key]), nil
}
func (b *publicationBackend) DeleteManaged(ctx context.Context, kind persistence.EntryKind, name, version string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := kind.Validate(); err != nil {
		return err
	}
	if name == "" || version == "" {
		return persistence.ErrInvalid
	}
	if !b.managed {
		return persistence.ErrNoManagedSource
	}
	key := [3]string{string(kind), name, version}
	if _, exists := b.entries[key]; !exists {
		return persistence.ErrNotFound
	}
	delete(b.entries, key)
	delete(b.times, key)
	b.recompute(kind, name)
	return nil
}

func (b *publicationBackend) recompute(kind persistence.EntryKind, name string) {
	best := ""
	for key := range b.entries {
		if key[0] == string(kind) && key[1] == name && (best == "" || model.CompareVersions(key[2], best) > 0) {
			best = key[2]
		}
	}
	for key, entry := range b.entries {
		if entry.Kind != kind || entry.Name != name {
			continue
		}
		entry.IsLatest = entry.Version == best
		if entry.Skill != nil {
			entry.Skill.ID, entry.Skill.IsLatest = entry.ID, entry.IsLatest
			entry.Skill.CreatedAt, entry.Skill.UpdatedAt = b.times[key], b.times[key]
		}
		if entry.Plugin != nil {
			entry.Plugin.ID, entry.Plugin.IsLatest = entry.ID, entry.IsLatest
			entry.Plugin.CreatedAt, entry.Plugin.UpdatedAt = b.times[key], b.times[key]
		}
		b.entries[key] = entry
	}
}
func (b *publicationBackend) ListSourceVersions(ctx context.Context, name string) ([]persistence.DiagnosticVersion, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !b.managed || name != "managed" {
		return nil, persistence.ErrNotFound
	}
	rows := []persistence.DiagnosticVersion{}
	for key, entry := range b.entries {
		row := persistence.DiagnosticVersion{Kind: entry.Kind, Name: entry.Name, Version: entry.Version, CreatedAt: b.times[key], UpdatedAt: b.times[key]}
		switch entry.Kind {
		case persistence.ServerKind:
			row.Title, row.Description = entry.Server.Title, entry.Server.Description
		case persistence.SkillKind:
			row.Title, row.Description = entry.Skill.Title, entry.Skill.Description
		case persistence.PluginKind:
			row.Title, row.Description = entry.Plugin.Title, entry.Plugin.Description
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		a, c := rows[i], rows[j]
		if a.Name != c.Name {
			return a.Name < c.Name
		}
		if a.Version != c.Version {
			return a.Version < c.Version
		}
		return a.Kind < c.Kind
	})
	return rows, nil
}
func (b *publicationBackend) ListViewVersions(ctx context.Context, name string) ([]persistence.ViewDiagnosticVersion, error) {
	if name != "view" {
		return nil, persistence.ErrNotFound
	}
	rows, err := b.ListSourceVersions(ctx, "managed")
	if err != nil {
		return nil, err
	}
	view := make([]persistence.ViewDiagnosticVersion, 0, len(rows))
	for _, row := range rows {
		view = append(view, persistence.ViewDiagnosticVersion{DiagnosticVersion: row, SourceName: "managed", SourceID: "host-managed-incarnation"})
	}
	return view, nil
}
func copyEntry(e persistence.Entry) persistence.Entry {
	data, err := json.Marshal(e)
	if err != nil {
		panic(err)
	}
	var out persistence.Entry
	if err = json.Unmarshal(data, &out); err != nil {
		panic(err)
	}
	return out
}

// Status is independently supplied, not a Jobs capability with hidden methods.
type ownStatus struct{}

func (ownStatus) Status(ctx context.Context, name string) (persistence.JobStatus, error) {
	if err := ctx.Err(); err != nil {
		return persistence.JobStatus{}, err
	}
	if name != "managed" {
		return persistence.JobStatus{}, persistence.ErrNotFound
	}
	return persistence.JobStatus{SourceID: "host-managed-incarnation", Phase: persistence.JobPending}, nil
}

// Replace the application implementations, not just their storage.
type ownPublisher struct{ backend *publicationBackend }

func (a ownPublisher) Publish(c context.Context, e persistence.Entry) (persistence.Entry, error) {
	return a.backend.Publish(c, e)
}
func (a ownPublisher) DeleteManaged(c context.Context, k persistence.EntryKind, n, v string) error {
	return a.backend.DeleteManaged(c, k, n, v)
}

type ownDiagnostics struct {
	backend *publicationBackend
	status  ownStatus
}

func (a ownDiagnostics) ListSourceVersions(c context.Context, n string) ([]persistence.DiagnosticVersion, error) {
	return a.backend.ListSourceVersions(c, n)
}
func (a ownDiagnostics) ListViewVersions(c context.Context, n string) ([]persistence.ViewDiagnosticVersion, error) {
	return a.backend.ListViewVersions(c, n)
}
func (a ownDiagnostics) Status(c context.Context, n string) (persistence.JobStatus, error) {
	return a.status.Status(c, n)
}

var _ persistence.ManagedPublisher = (*publicationBackend)(nil)
var _ persistence.EntryDiagnostics = (*publicationBackend)(nil)
var _ persistence.SourceStatusReader = ownStatus{}
var _ catalog.Publisher = ownPublisher{}
var _ catalog.Diagnostics = ownDiagnostics{}

func TestPublicPublicationDiagnosticsAndFormats(t *testing.T) {
	t.Parallel()
	for _, implementation := range []string{"default", "replacement"} {
		t.Run(implementation, func(t *testing.T) {
			t.Parallel()
			backend := &publicationBackend{entries: map[[3]string]persistence.Entry{}, times: map[[3]string]time.Time{}}
			var publisher catalog.Publisher
			var diag catalog.Diagnostics
			var err error
			if implementation == "default" {
				publisher, err = catalog.NewPublisher(backend)
				if err != nil {
					t.Fatal(err)
				}
				diag, err = catalog.NewDiagnostics(backend, ownStatus{})
				if err != nil {
					t.Fatal(err)
				}
			} else {
				publisher, diag = ownPublisher{backend}, ownDiagnostics{backend, ownStatus{}}
			}
			server := &model.Server{Schema: "https://example.org/server.schema.json", Name: "com.example/item", Version: "1", Title: "server", Description: "description"}
			skill := &model.Skill{Namespace: "com.example", Name: "item", Version: "1", Title: "skill", Description: "description", Metadata: map[string]any{"host": "metadata"}, AllowedTools: []string{"tool"}}
			plugin := &model.Plugin{Namespace: "com.example", Name: "item", Version: "1", Title: "plugin", Description: "description", Metadata: map[string]any{"host": "metadata"}}
			inputs := []persistence.Entry{
				{Kind: persistence.ServerKind, Name: server.Name, Version: "1", Server: server},
				{Kind: persistence.SkillKind, Name: skill.Name, Version: "1", Skill: skill},
				{Kind: persistence.PluginKind, Name: plugin.Name, Version: "1", Plugin: plugin},
			}
			if _, err = publisher.Publish(t.Context(), inputs[0]); !errors.Is(err, persistence.ErrNoManagedSource) || !errors.Is(err, persistence.ErrNotFound) {
				t.Fatal(err)
			}
			backend.managed = true // Fixture-only setup, not publisher auto-creation.
			for _, input := range inputs {
				stored, e := publisher.Publish(t.Context(), input)
				if e != nil || stored.ID == "" || stored.SourceID != "host-managed-incarnation" {
					t.Fatal(stored, e)
				}
				switch input.Kind {
				case persistence.ServerKind:
					if !reflect.DeepEqual(formats.ServerPayload(stored.Server).Server, *input.Server) {
						t.Fatal("server wire changed")
					}
				case persistence.SkillKind:
					if !reflect.DeepEqual(formats.SkillPayload(stored.Skill), formats.SkillPayload(input.Skill)) {
						t.Fatal("skill wire changed")
					}
				case persistence.PluginKind:
					if !reflect.DeepEqual(formats.PluginPayload(stored.Plugin), formats.PluginPayload(input.Plugin)) {
						t.Fatal("plugin wire changed")
					}
				}
				if _, e = publisher.Publish(t.Context(), input); !errors.Is(e, persistence.ErrConflict) {
					t.Fatal(e)
				}
			}
			rows, err := diag.ListSourceVersions(t.Context(), "managed")
			if err != nil || len(rows) != 3 {
				t.Fatal(rows, err)
			}
			view, err := diag.ListViewVersions(t.Context(), "view")
			if err != nil || len(view) != 3 || view[1].Kind != persistence.PluginKind || view[2].Kind != persistence.SkillKind {
				t.Fatal(view, err)
			}
			for i, row := range rows {
				if row.CreatedAt.IsZero() || row.Title == "" || row.Description == "" || view[i].DiagnosticVersion != row || view[i].SourceID != "host-managed-incarnation" {
					t.Fatal("wire projection not faithful", row, view[i])
				}
			}
			status, err := diag.Status(t.Context(), "managed")
			if err != nil || status.Phase != persistence.JobPending || status.SourceID != "host-managed-incarnation" {
				t.Fatal(status, err)
			}
			if err = publisher.DeleteManaged(t.Context(), persistence.PluginKind, "item", "1"); err != nil {
				t.Fatal(err)
			}
			rows, err = diag.ListSourceVersions(t.Context(), "managed")
			if err != nil || len(rows) != 2 {
				t.Fatal(rows, err)
			}
		})
	}
}
