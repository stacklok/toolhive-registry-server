package conformance

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	upstreammodel "github.com/modelcontextprotocol/registry/pkg/model"
	thvregistry "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

const (
	consumerView         = "consumer"
	viewCopyKey          = "copy"
	viewOriginalMetadata = "original"
	changedViewPayload   = "changed-returned-payload"
)

// ViewEntriesFactory returns independent fresh definition, write and view-read
// capabilities. RunViewEntries executes through both persistence and catalog.
type ViewEntriesFactory func(*testing.T) (persistence.Definitions, persistence.Entries, persistence.ViewEntryReader)

// RunViewEntries checks selection before every filter and bound on every kind.
//
//nolint:gocyclo,lll // Independent table cases keep all-kind winner traps and boundary assertions together.
func RunViewEntries(t *testing.T, factory ViewEntriesFactory) {
	t.Helper()
	for _, kind := range []persistence.EntryKind{persistence.ServerKind, persistence.SkillKind, persistence.PluginKind} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			t.Run("whole-name winners", func(t *testing.T) {
				t.Parallel()
				defs, entries, views := factory(t)
				ctx := t.Context()
				a := newEntrySource(t, defs, "priority-a")
				b := newEntrySource(t, defs, "priority-b")
				reader, err := catalog.NewReader(views)
				if err != nil {
					t.Fatal(err)
				}
				name := viewEntryName(kind, "item")
				snap := model.Snapshot{}
				appendViewVersion(&snap, kind, name, "2", "org.older", "winner %_\\ literal")
				appendViewVersion(&snap, kind, name, "3", "org.winner", "winner description")
				appendViewVersion(&snap, kind, viewEntryName(kind, "z-last"), "2", "org.winner", "last")
				if err = entries.ReplaceSnapshot(ctx, a, snap); err != nil {
					t.Fatal(err)
				}
				since := time.Now().UTC()
				time.Sleep(2 * time.Millisecond)
				snap = model.Snapshot{}
				for _, version := range []string{"1", "2", "9"} {
					appendViewVersion(&snap, kind, name, version, "org.loser", "lower-only-search")
				}
				appendViewVersion(&snap, kind, viewEntryName(kind, "z-last"), "1", "org.loser", "last")
				// A server and a skill/plugin with the same name must not share ownership.
				otherKind := persistence.SkillKind
				if kind == otherKind {
					otherKind = persistence.PluginKind
				}
				otherName := name
				if kind == persistence.ServerKind {
					otherName = "item"
				}
				appendViewVersion(&snap, otherKind, otherName, "9", "org.loser", "other-kind")
				if err = entries.ReplaceSnapshot(ctx, b, snap); err != nil {
					t.Fatal(err)
				}
				createView(t, defs, consumerView, []string{a.Name, b.Name})
				identity := catalog.EntryName{Kind: kind, Name: name}
				got, err := reader.Get(ctx, consumerView, identity, catalog.VersionSelector{Latest: true})
				if err != nil || got.SourceID != a.ID || got.Version != "3" {
					t.Fatalf("latest: %+v / %v", got, err)
				}
				_, err = reader.Get(ctx, consumerView, identity, catalog.VersionSelector{Exact: "1"})
				if !errors.Is(err, persistence.ErrNotFound) {
					t.Fatalf("lower exact version filled gap: %v", err)
				}
				got, err = reader.Get(ctx, consumerView, catalog.EntryName{Kind: otherKind, Name: otherName}, catalog.VersionSelector{Latest: true})
				if err != nil || got.SourceID != b.ID {
					t.Fatalf("kind isolation: %+v / %v", got, err)
				}
				for _, q := range []catalog.Query{
					{Kind: kind, Name: name, Search: "lower-only", Limit: 2},
					{Kind: kind, Name: name, UpdatedSince: &since, Limit: 2},
					{Kind: kind, Name: name, VersionSelector: catalog.VersionSelector{Exact: "1"}, Limit: 2},
					{Kind: kind, Name: name, Search: "%", VersionSelector: catalog.VersionSelector{Latest: true}, Limit: 2},
					{Kind: kind, Name: name, Search: "lower-only", VersionSelector: catalog.VersionSelector{Latest: true}, Limit: 2},
				} {
					page, e := reader.List(ctx, consumerView, q)
					if e != nil || len(page.Entries) != 0 || page.NextCursor != "" {
						t.Fatalf("losing filter promoted source: %+v / %+v / %v", q, page, e)
					}
				}
				if kind != persistence.ServerKind {
					identity.Namespace = "org.loser"
					_, e := reader.Get(ctx, consumerView, identity, catalog.VersionSelector{Latest: true})
					if !errors.Is(e, persistence.ErrNotFound) {
						t.Fatalf("namespace promoted source: %v", e)
					}
					page, e := reader.ListVersions(ctx, consumerView, identity, catalog.PageRequest{Limit: 2})
					if e != nil || len(page.Entries) != 0 {
						t.Fatalf("namespace history promoted source: %+v / %v", page, e)
					}
					page, e = reader.List(ctx, consumerView, catalog.Query{Kind: kind, Name: name, Namespace: identity.Namespace, Limit: 2})
					if e != nil || len(page.Entries) != 0 {
						t.Fatalf("namespace list promoted source: %+v / %v", page, e)
					}
					identity.Namespace = "org.older"
					_, e = reader.Get(ctx, consumerView, identity, catalog.VersionSelector{Latest: true})
					if !errors.Is(e, persistence.ErrNotFound) {
						t.Fatalf("namespace reselected older latest: %v", e)
					}
					page, e = reader.List(ctx, consumerView, catalog.Query{
						Kind: kind, Name: name, Namespace: identity.Namespace, VersionSelector: catalog.VersionSelector{Latest: true}, Limit: 2,
					})
					if e != nil || len(page.Entries) != 0 {
						t.Fatalf("namespace reselected older latest list: %+v / %v", page, e)
					}
					identity.Namespace = ""
				}
				for _, literal := range []string{"%", "_", "\\", "WINNER"} {
					page, e := reader.List(ctx, consumerView, catalog.Query{Kind: kind, Search: literal, Limit: 1000})
					want := 1
					if literal == "WINNER" {
						want = 2
					}
					if e != nil || len(page.Entries) != want {
						t.Fatalf("literal search %q: %+v / %v", literal, page, e)
					}
				}
				history, e := reader.ListVersions(ctx, consumerView, identity, catalog.PageRequest{Limit: 1000})
				if e != nil || len(history.Entries) != 2 || history.Entries[0].Version != "2" || history.Entries[1].Version != "3" {
					t.Fatalf("whole history: %+v / %v", history, e)
				}
				for _, limit := range []int{1, 2, 1000} {
					q := catalog.Query{Kind: kind, Limit: limit}
					all := walkView(t, reader, consumerView, q)
					if len(all) != 3 || all[0].SourceID != a.ID || all[1].SourceID != a.ID || all[2].SourceID != a.ID || all[2].Version != "2" || all[2].Name != viewEntryName(kind, "z-last") {
						t.Fatalf("short/shadowed page at %d: %+v", limit, all)
					}
				}
				// Returned payloads must be owned values, not aliases into backend state.
				page, e := reader.List(ctx, consumerView, catalog.Query{Kind: kind, Name: name, Limit: 2})
				if e != nil {
					t.Fatal(e)
				}
				mutateViewPayload(page.Entries[0])
				again, e := reader.Get(ctx, consumerView, catalog.EntryName{Kind: kind, Name: name}, catalog.VersionSelector{Exact: "2"})
				if e != nil || viewDescription(again) != "winner %_\\ literal" {
					t.Fatalf("payload alias: %+v / %v", again, e)
				}
				if again.Server != nil && again.Server.Icons[0].Sizes[0] != "any" ||
					again.Skill != nil && again.Skill.Metadata[viewCopyKey] != viewOriginalMetadata ||
					again.Plugin != nil && again.Plugin.Metadata[viewCopyKey] != viewOriginalMetadata {
					t.Fatal("returned metadata aliases stored payload")
				}
				// Live continuation recomputes ownership after the winning source empties.
				first, e := reader.List(ctx, consumerView, catalog.Query{Kind: kind, Limit: 1})
				if e != nil {
					t.Fatal(e)
				}
				if e = entries.ReplaceSnapshot(ctx, a, model.Snapshot{}); e != nil {
					t.Fatal(e)
				}
				next, e := reader.List(ctx, consumerView, catalog.Query{Kind: kind, Limit: 1, Cursor: first.NextCursor})
				if e != nil || len(next.Entries) != 1 || next.Entries[0].SourceID != b.ID || next.Entries[0].Version != "9" {
					t.Fatalf("live winner mutation: %+v / %v", next, e)
				}
				latest, e := reader.Get(ctx, consumerView, catalog.EntryName{Kind: kind, Name: name}, catalog.VersionSelector{Latest: true})
				if e != nil || latest.SourceID != b.ID {
					t.Fatalf("empty-source ownership: %+v / %v", latest, e)
				}
			})
			t.Run("bounds and histories", func(t *testing.T) {
				t.Parallel()
				defs, entries, views := factory(t)
				source := newEntrySource(t, defs, "histories")
				snap := model.Snapshot{}
				for _, count := range []int{29, 30, 31, 999, 1000, 1001} {
					for i := range count {
						appendViewVersion(&snap, kind, viewEntryName(kind, fmt.Sprintf("history-%d", count)), fmt.Sprintf("%04d", i), "com.example", "history")
					}
				}
				if e := entries.ReplaceSnapshot(t.Context(), source, snap); e != nil {
					t.Fatal(e)
				}
				createView(t, defs, consumerView, []string{source.Name})
				reader, e := catalog.NewReader(views)
				if e != nil {
					t.Fatal(e)
				}
				for _, count := range []int{29, 30, 31, 999, 1000, 1001} {
					limit := 1000
					if count < 100 {
						limit = 30
					}
					name := catalog.EntryName{Kind: kind, Name: viewEntryName(kind, fmt.Sprintf("history-%d", count))}
					page, e := reader.ListVersions(t.Context(), consumerView, name, catalog.PageRequest{Limit: limit})
					if e != nil || len(page.Entries) != min(count, limit) || (page.NextCursor != "") != (count > limit) {
						t.Fatalf("history %d bound %d: %d / %q / %v", count, limit, len(page.Entries), page.NextCursor, e)
					}
					if page.NextCursor != "" {
						next, e := reader.ListVersions(t.Context(), consumerView, name, catalog.PageRequest{Limit: limit, Cursor: page.NextCursor})
						if e != nil || len(next.Entries) != count-limit || next.NextCursor != "" {
							t.Fatalf("history tail: %+v / %v", next, e)
						}
					}
				}
			})
			t.Run("concurrent link selection", func(t *testing.T) {
				t.Parallel()
				runViewConcurrentLinks(t, factory, kind)
			})
			t.Run("versions and invalid cursors", func(t *testing.T) {
				t.Parallel()
				runViewCursors(t, factory, kind)
			})
		})
	}
}

//nolint:gocyclo,lll // Each query/link/incarnation mutation independently checks token rejection.
func runViewCursors(t *testing.T, factory ViewEntriesFactory, kind persistence.EntryKind) {
	t.Helper()
	defs, entries, views := factory(t)
	ctx := t.Context()
	a := newEntrySource(t, defs, "cursor-a")
	b := newEntrySource(t, defs, "cursor-b")
	name := viewEntryName(kind, "item")
	versions := []string{testLiteralLatest, "zzz", "1.0", "v1.0.0", "1.0.0+aaa", "1.0.0+bbb", "1.0.0-rc.1"}
	snap := model.Snapshot{}
	for _, v := range versions {
		appendViewVersion(&snap, kind, name, v, "com.example", "cursor")
	}
	if e := entries.ReplaceSnapshot(ctx, a, snap); e != nil {
		t.Fatal(e)
	}
	createView(t, defs, consumerView, []string{a.Name, b.Name})
	createView(t, defs, "other", []string{a.Name, b.Name})
	reader, e := catalog.NewReader(views)
	if e != nil {
		t.Fatal(e)
	}
	best := versions[0]
	for _, v := range versions {
		if model.CompareVersions(v, best) > 0 {
			best = v
		}
	}
	got, e := reader.Get(ctx, consumerView, catalog.EntryName{Kind: kind, Name: name}, catalog.VersionSelector{Latest: true})
	if e != nil || got.Version != best {
		t.Fatalf("approved comparator: %+v / %v; want %s", got, e, best)
	}
	got, e = reader.Get(ctx, consumerView, catalog.EntryName{Kind: kind, Name: name}, catalog.VersionSelector{Exact: testLiteralLatest})
	if e != nil || got.Version != testLiteralLatest || got.IsLatest {
		t.Fatalf("literal latest: %+v / %v", got, e)
	}
	q := catalog.Query{Kind: kind, Limit: 1}
	first, e := reader.List(ctx, consumerView, q)
	if e != nil || first.NextCursor == "" {
		t.Fatalf("cursor first: %+v / %v", first, e)
	}
	sourcePage, e := entries.ListEntries(ctx, persistence.ListOptions{SourceID: a.ID, Kind: kind, Limit: 1})
	if e != nil || sourcePage.NextCursor == "" {
		t.Fatalf("source cursor fixture: %+v / %v", sourcePage, e)
	}
	wrongBackendQuery := q
	wrongBackendQuery.Cursor = sourcePage.NextCursor
	_, e = reader.List(ctx, consumerView, wrongBackendQuery)
	if !errors.Is(e, persistence.ErrInvalid) {
		t.Fatalf("source token accepted as view token: %v", e)
	}
	for _, limit := range []int{-1, 0, 1001} {
		bad := q
		bad.Limit = limit
		_, e = reader.List(ctx, consumerView, bad)
		if !errors.Is(e, persistence.ErrInvalid) {
			t.Fatalf("invalid limit %d: %v", limit, e)
		}
		_, e = reader.ListVersions(ctx, consumerView, catalog.EntryName{Kind: kind, Name: name}, catalog.PageRequest{Limit: limit})
		if !errors.Is(e, persistence.ErrInvalid) {
			t.Fatalf("invalid history bound: %v", e)
		}
	}
	for _, bad := range []catalog.Query{
		{Kind: "bad", Limit: 1}, {Kind: kind, Limit: 1, VersionSelector: catalog.VersionSelector{Exact: "1", Latest: true}},
		{Kind: persistence.ServerKind, Namespace: "com.example", Limit: 1},
	} {
		_, e = views.ListViewEntries(ctx, consumerView, bad)
		if !errors.Is(e, persistence.ErrInvalid) {
			t.Fatalf("invalid query: %v", e)
		}
	}
	raw, _ := base64.RawURLEncoding.DecodeString(first.NextCursor)
	var token map[string]any
	if e = json.Unmarshal(raw, &token); e != nil {
		t.Fatal(e)
	}
	// Backend format fields differ deliberately; both reject future versions.
	if _, ok := token["Version"]; ok {
		token["Version"] = 99
	} else {
		token["Format"] = 99
	}
	raw, _ = json.Marshal(token)
	for _, cursor := range []string{"!", strings.Repeat("a", 4097), base64.RawURLEncoding.EncodeToString(raw), base64.RawURLEncoding.EncodeToString([]byte(`{"AfterName":"item","AfterVersion":"1"}`)), base64.RawURLEncoding.EncodeToString([]byte(`{"name":"item","version":"1"}`))} {
		bad := q
		bad.Cursor = cursor
		_, e = reader.List(ctx, consumerView, bad)
		if !errors.Is(e, persistence.ErrInvalid) {
			t.Fatalf("invalid token accepted: %v", e)
		}
	}
	now := time.Now()
	changes := []catalog.Query{q, q, q, q, q, q, q}
	changes[0].Name = name
	changes[1].Search = "cursor"
	changes[2].Limit = 2
	changes[3].VersionSelector.Exact = testLiteralLatest
	changes[4].UpdatedSince = &now
	changes[5].VersionSelector.Latest = true
	changes[6].Kind = persistence.SkillKind
	if kind == persistence.SkillKind {
		changes[6].Kind = persistence.PluginKind
	}
	if kind != persistence.ServerKind {
		bad := q
		bad.Namespace = "com.example"
		changes = append(changes, bad)
	}
	for _, bad := range changes {
		bad.Cursor = first.NextCursor
		_, e = reader.List(ctx, consumerView, bad)
		if !errors.Is(e, persistence.ErrInvalid) {
			t.Fatalf("query changed accepted %+v / %v", bad, e)
		}
	}
	bad := q
	bad.Cursor = first.NextCursor
	_, e = reader.List(ctx, "other", bad)
	if !errors.Is(e, persistence.ErrInvalid) {
		t.Fatalf("other view token: %v", e)
	}
	if _, e = defs.UpdateView(ctx, consumerView, persistence.ViewDefinition{Name: consumerView, Sources: []string{b.Name, a.Name}}); e != nil {
		t.Fatal(e)
	}
	_, e = reader.List(ctx, consumerView, bad)
	if !errors.Is(e, persistence.ErrInvalid) {
		t.Fatalf("reordered links: %v", e)
	}
	if e = defs.DeleteView(ctx, consumerView); e != nil {
		t.Fatal(e)
	}
	createView(t, defs, consumerView, []string{a.Name, b.Name})
	_, e = reader.List(ctx, consumerView, bad)
	if !errors.Is(e, persistence.ErrInvalid) {
		t.Fatalf("view recreation: %v", e)
	}
	first, e = reader.List(ctx, consumerView, q)
	if e != nil {
		t.Fatal(e)
	}
	bad.Cursor = first.NextCursor
	if _, e = defs.UpdateView(ctx, consumerView, persistence.ViewDefinition{Name: consumerView, Sources: []string{b.Name}}); e != nil {
		t.Fatal(e)
	}
	if e = defs.DeleteView(ctx, "other"); e != nil {
		t.Fatal(e)
	}
	if e = defs.DeleteSource(ctx, a.Name); e != nil {
		t.Fatal(e)
	}
	a = newEntrySource(t, defs, a.Name)
	if _, e = defs.UpdateView(ctx, consumerView, persistence.ViewDefinition{Name: consumerView, Sources: []string{a.Name, b.Name}}); e != nil {
		t.Fatal(e)
	}
	_, e = reader.List(ctx, consumerView, bad)
	if !errors.Is(e, persistence.ErrInvalid) {
		t.Fatalf("source recreation: %v", e)
	}
	empty, e := reader.List(ctx, consumerView, q)
	if e != nil || len(empty.Entries) != 0 {
		t.Fatalf("empty view: %+v / %v", empty, e)
	}
	empty, e = reader.ListVersions(ctx, consumerView, catalog.EntryName{Kind: kind, Name: name}, catalog.PageRequest{Limit: 1})
	if e != nil || len(empty.Entries) != 0 {
		t.Fatalf("empty history: %+v / %v", empty, e)
	}
	_, e = reader.List(ctx, "missing", q)
	if !errors.Is(e, persistence.ErrNotFound) {
		t.Fatalf("missing view: %v", e)
	}
	_, e = reader.Get(ctx, consumerView, catalog.EntryName{Kind: kind, Name: name}, catalog.VersionSelector{Latest: true})
	if !errors.Is(e, persistence.ErrNotFound) {
		t.Fatalf("missing entry: %v", e)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, e = reader.List(canceled, consumerView, q)
	if !errors.Is(e, context.Canceled) {
		t.Fatalf("canceled list: %v", e)
	}
	_, e = reader.Get(canceled, consumerView, catalog.EntryName{Kind: kind, Name: name}, catalog.VersionSelector{Latest: true})
	if !errors.Is(e, context.Canceled) {
		t.Fatalf("canceled get: %v", e)
	}
	_, e = reader.ListVersions(canceled, consumerView, catalog.EntryName{Kind: kind, Name: name}, catalog.PageRequest{Limit: 1})
	if !errors.Is(e, context.Canceled) {
		t.Fatalf("canceled history: %v", e)
	}
	// Oversized filters remain valid queries; they are not truncated to a
	// matching prefix. Raw legacy fixtures separately exercise emitted tokens.
	long := strings.Repeat("x", 5000)
	snap = model.Snapshot{}
	for _, v := range []string{"1", "2"} {
		appendViewVersion(&snap, kind, name, v, "com.example", "cursor")
	}
	if e = entries.ReplaceSnapshot(ctx, a, snap); e != nil {
		t.Fatal(e)
	}
	large := q
	large.Search = long
	page, e := reader.List(ctx, consumerView, large)
	if e != nil || len(page.Entries) != 0 || page.NextCursor != "" {
		t.Fatalf("long filter: %+v / %v", page, e)
	}
}

//nolint:gocyclo // Concurrent writes and full-page invariants are checked without terminating the worker goroutine.
func runViewConcurrentLinks(t *testing.T, factory ViewEntriesFactory, kind persistence.EntryKind) {
	t.Helper()
	defs, entries, views := factory(t)
	ctx := t.Context()
	a := newEntrySource(t, defs, "concurrent-a")
	b := newEntrySource(t, defs, "concurrent-b")
	name := viewEntryName(kind, "item")
	for _, source := range []persistence.SourceDefinition{a, b} {
		snap := model.Snapshot{}
		versions := []string{"2", "3"}
		if source.ID == b.ID {
			versions = []string{"1", "9"}
		}
		for _, version := range versions {
			appendViewVersion(&snap, kind, name, version, "com.example", "concurrent")
		}
		if err := entries.ReplaceSnapshot(ctx, source, snap); err != nil {
			t.Fatal(err)
		}
	}
	createView(t, defs, consumerView, []string{a.Name, b.Name})
	done := make(chan error, 1)
	go func() {
		for i := range 40 {
			links := []string{a.Name, b.Name}
			if i%2 == 0 {
				links = []string{b.Name, a.Name}
			}
			if _, err := defs.UpdateView(ctx, consumerView, persistence.ViewDefinition{Name: consumerView, Sources: links}); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	var readErr error
	for range 40 {
		page, err := views.ListViewEntries(ctx, consumerView, persistence.ViewQuery{Kind: kind, Limit: 2})
		if err != nil {
			readErr = err
			break
		}
		if len(page.Entries) != 2 || page.NextCursor != "" || page.Entries[0].SourceID != page.Entries[1].SourceID {
			readErr = fmt.Errorf("mixed concurrent view page: %+v", page)
			break
		}
		first, last := "2", "3"
		if page.Entries[0].SourceID == b.ID {
			first, last = "1", "9"
		}
		if page.Entries[0].Version != first || page.Entries[1].Version != last {
			readErr = fmt.Errorf("mixed version histories: %+v", page)
			break
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if readErr != nil {
		t.Fatal(readErr)
	}
}

func createView(t *testing.T, defs persistence.Views, name string, sources []string) {
	t.Helper()
	if _, e := defs.CreateView(t.Context(), persistence.ViewDefinition{Name: name, Sources: sources}); e != nil {
		t.Fatal(e)
	}
}
func viewEntryName(kind persistence.EntryKind, name string) string {
	if kind == persistence.ServerKind {
		return "com.example/" + name
	}
	return name
}
func appendViewVersion(snap *model.Snapshot, kind persistence.EntryKind, name, version, namespace, description string) {
	switch kind {
	case persistence.ServerKind:
		snap.Data.Servers = append(snap.Data.Servers, model.Server{
			Schema: "https://example.org/server.schema.json", Name: name, Version: version, Description: description,
			Icons: []upstreammodel.Icon{{Src: "https://example.org/icon.png", Sizes: []string{"any"}}},
		})
	case persistence.SkillKind:
		snap.Data.Skills = append(snap.Data.Skills, thvregistry.Skill{
			Namespace: namespace, Name: name, Version: version, Description: description,
			Metadata: map[string]any{viewCopyKey: viewOriginalMetadata},
		})
	case persistence.PluginKind:
		snap.Data.Plugins = append(snap.Data.Plugins, thvregistry.Plugin{
			Namespace: namespace, Name: name, Version: version, Description: description,
			Metadata: map[string]any{viewCopyKey: viewOriginalMetadata},
		})
	}
}
func walkView(t *testing.T, r catalog.Reader, view string, q catalog.Query) []persistence.Entry {
	t.Helper()
	var all []persistence.Entry
	for {
		page, e := r.List(t.Context(), view, q)
		if e != nil {
			t.Fatal(e)
		}
		if len(page.Entries) > q.Limit {
			t.Fatal("unbounded page")
		}
		all = append(all, page.Entries...)
		if page.NextCursor == "" {
			break
		}
		if q.Cursor == page.NextCursor {
			t.Fatal("cursor did not advance")
		}
		q.Cursor = page.NextCursor
	}
	for i := 1; i < len(all); i++ {
		a, b := all[i-1], all[i]
		if a.Name > b.Name || a.Name == b.Name && a.Version >= b.Version {
			t.Fatal("non-deterministic ordering", reflect.ValueOf(all))
		}
	}
	return all
}
func mutateViewPayload(v persistence.Entry) {
	if v.Server != nil {
		v.Server.Description = changedViewPayload
		v.Server.Icons[0].Sizes[0] = changedViewPayload
	}
	if v.Skill != nil {
		v.Skill.Description = changedViewPayload
		v.Skill.Metadata[viewCopyKey] = changedViewPayload
	}
	if v.Plugin != nil {
		v.Plugin.Description = changedViewPayload
		v.Plugin.Metadata[viewCopyKey] = changedViewPayload
	}
}
func viewDescription(v persistence.Entry) string {
	if v.Server != nil {
		return v.Server.Description
	}
	if v.Skill != nil {
		return v.Skill.Description
	}
	return v.Plugin.Description
}
