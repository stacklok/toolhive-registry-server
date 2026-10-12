package conformance

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	thvregistry "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

const (
	diagnosticMutation = "changed diagnostic"
	diagnosticMissing  = "missing"
)

// DiagnosticsFactory returns initialized capabilities over fresh shared state.
type DiagnosticsFactory func(*testing.T) (persistence.Definitions, persistence.Entries, persistence.EntryDiagnostics)

// TiedDiagnosticPositionFactory also supplies a test-only fixture hook for
// historical view links whose positions are equal.
type TiedDiagnosticPositionFactory func(*testing.T) (persistence.Definitions, persistence.Entries, persistence.EntryDiagnostics, func(context.Context, string) error)

// RunDiagnostics checks complete, unshadowed diagnostic projections independently
// of consumer dedup. Backends must use one snapshot for definitions/links/rows.
//
//nolint:gocyclo // Independent cases exercise complete multi-kind listing and consistency semantics.
func RunDiagnostics(t *testing.T, factory DiagnosticsFactory) {
	t.Helper()
	t.Run("all versions and shadowed duplicates", func(t *testing.T) {
		t.Parallel()
		defs, entries, diag := factory(t)
		ctx := t.Context()
		a := newEntrySource(t, defs, "alpha")
		b := newEntrySource(t, defs, "beta")
		createView(t, defs, "diagnostic", []string{b.Name, a.Name})
		var snapshot model.Snapshot
		for _, version := range []string{testLiteralLatest, "2", "10", "1"} {
			server := conformanceServer(version)
			server.Title = "server title"
			snapshot.Data.Servers = append(snapshot.Data.Servers, server)
			skill := conformanceSkill(version)
			skill.Title = "skill title"
			snapshot.Data.Skills = append(snapshot.Data.Skills, skill)
			plugin := conformancePlugin(version)
			plugin.Title = "plugin title"
			snapshot.Data.Plugins = append(snapshot.Data.Plugins, plugin)
		}
		for _, source := range []persistence.SourceDefinition{a, b} {
			if err := entries.ReplaceSnapshot(ctx, source, snapshot); err != nil {
				t.Fatal(err)
			}
		}
		start := time.Now().UTC()
		rows, err := diag.ListSourceVersions(ctx, a.Name)
		if err != nil || len(rows) != 12 {
			t.Fatalf("source rows: %+v / %v", rows, err)
		}
		if !sort.SliceIsSorted(rows, func(i, j int) bool { return diagnosticKey(rows[i]) < diagnosticKey(rows[j]) }) {
			t.Fatal("source is not byte-lexically ordered")
		}
		for _, row := range rows {
			if row.Title == "" || row.Description == "" || row.CreatedAt.IsZero() || row.UpdatedAt.IsZero() ||
				row.CreatedAt.After(start) || row.CreatedAt.Location() != time.UTC || row.UpdatedAt.Location() != time.UTC {
				t.Fatalf("missing or invented metadata: %+v", row)
			}
		}
		original := append([]persistence.DiagnosticVersion(nil), rows...)
		rows[0].Description, rows[0].Version = diagnosticMutation, diagnosticMutation
		again, err := diag.ListSourceVersions(ctx, a.Name)
		if err != nil || !reflect.DeepEqual(original, again) {
			t.Fatalf("aliased or read touched timestamps: %+v / %v", again, err)
		}
		view, err := diag.ListViewVersions(ctx, "diagnostic")
		if err != nil || len(view) != 24 {
			t.Fatalf("view silently deduped: %+v / %v", view, err)
		}
		for i, row := range original {
			first, second := view[2*i], view[2*i+1]
			if first.Name != row.Name || first.Kind != row.Kind || first.Version != row.Version ||
				first.SourceName != b.Name || first.SourceID != b.ID || first.Position != 0 ||
				second.SourceName != a.Name || second.SourceID != a.ID || second.Position != 1 || second.DiagnosticVersion != row {
				t.Fatalf("kind/position ordering or fidelity: %+v / %+v", first, second)
			}
		}
		view[0].SourceID, view[0].Title = diagnosticMutation, diagnosticMutation
		freshView, err := diag.ListViewVersions(ctx, "diagnostic")
		if err != nil || freshView[0].SourceID != b.ID || freshView[0].Title == diagnosticMutation {
			t.Fatal("aliased view")
		}
	})
	t.Run("complete not bounded consumer pages", func(t *testing.T) {
		t.Parallel()
		defs, entries, diag := factory(t)
		source := newEntrySource(t, defs, "complete")
		createView(t, defs, "complete", []string{source.Name})
		snapshot := model.Snapshot{}
		for i := range 1101 {
			skill := conformanceSkill(fmt.Sprintf("v%04d", i))
			snapshot.Data.Skills = append(snapshot.Data.Skills, skill)
		}
		if err := entries.ReplaceSnapshot(t.Context(), source, snapshot); err != nil {
			t.Fatal(err)
		}
		rows, err := diag.ListSourceVersions(t.Context(), source.Name)
		if err != nil || len(rows) != 1101 {
			t.Fatalf("source truncated: %d / %v", len(rows), err)
		}
		view, err := diag.ListViewVersions(t.Context(), "complete")
		if err != nil || len(view) != 1101 {
			t.Fatalf("view truncated: %d / %v", len(view), err)
		}
	})
	t.Run("empty missing and context", func(t *testing.T) {
		t.Parallel()
		defs, _, diag := factory(t)
		source := newEntrySource(t, defs, "empty")
		createView(t, defs, "empty", []string{source.Name})
		rows, err := diag.ListSourceVersions(t.Context(), source.Name)
		if err != nil || rows == nil || len(rows) != 0 {
			t.Fatalf("existing empty source: %+v / %v", rows, err)
		}
		view, err := diag.ListViewVersions(t.Context(), "empty")
		if err != nil || view == nil || len(view) != 0 {
			t.Fatalf("existing empty view: %+v / %v", view, err)
		}
		for _, name := range []string{diagnosticMissing, "x' OR true --"} {
			if _, err = diag.ListSourceVersions(t.Context(), name); !errors.Is(err, persistence.ErrNotFound) {
				t.Fatal(err)
			}
			if _, err = diag.ListViewVersions(t.Context(), name); !errors.Is(err, persistence.ErrNotFound) {
				t.Fatal(err)
			}
		}
		cancelled, cancel := context.WithCancel(t.Context())
		cancel()
		deadline, done := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
		defer done()
		for _, ctx := range []context.Context{cancelled, deadline} {
			if _, err = diag.ListSourceVersions(ctx, source.Name); !errors.Is(err, ctx.Err()) {
				t.Fatal(err)
			}
			if _, err = diag.ListViewVersions(ctx, "empty"); !errors.Is(err, ctx.Err()) {
				t.Fatal(err)
			}
		}
	})
	t.Run("coherent link mutation", func(t *testing.T) {
		t.Parallel()
		defs, entries, diag := factory(t)
		a := newEntrySource(t, defs, "link-a")
		b := newEntrySource(t, defs, "link-b")
		snapshot := model.Snapshot{}
		snapshot.Data.Skills = []thvregistry.Skill{conformanceSkill("1"), conformanceSkill("2")}
		for _, source := range []persistence.SourceDefinition{a, b} {
			if err := entries.ReplaceSnapshot(t.Context(), source, snapshot); err != nil {
				t.Fatal(err)
			}
		}
		createView(t, defs, "links", []string{a.Name, b.Name})
		updates := make(chan error, 1)
		go func() {
			for i := range 20 {
				links := []string{a.Name, b.Name}
				if i%2 == 0 {
					links = []string{b.Name, a.Name}
				}
				if _, err := defs.UpdateView(t.Context(), "links", persistence.ViewDefinition{Name: "links", Sources: links}); err != nil {
					updates <- err
					return
				}
			}
			updates <- nil
		}()
		var readErr error
		for range 20 {
			rows, err := diag.ListViewVersions(t.Context(), "links")
			if err != nil || len(rows) != 4 {
				readErr = fmt.Errorf("incoherent length %d: %w", len(rows), err)
				break
			}
			if rows[0].Position != 0 || rows[1].Position != 1 || rows[2].Position != 0 || rows[3].Position != 1 ||
				rows[0].SourceID != rows[2].SourceID || rows[1].SourceID != rows[3].SourceID || rows[0].SourceID == rows[1].SourceID {
				readErr = fmt.Errorf("incoherent links: %+v", rows)
				break
			}
		}
		writeErr := <-updates
		if readErr != nil || writeErr != nil {
			t.Fatalf("reads %v / updates %v", readErr, writeErr)
		}
	})
}

// RunDiagnosticsTiedPositions verifies ordering of historical links that share
// a position. This state cannot be created through the public definitions API.
func RunDiagnosticsTiedPositions(t *testing.T, factory TiedDiagnosticPositionFactory) {
	t.Helper()
	defs, entries, diag, tiePositions := factory(t)
	ctx := t.Context()
	a := newEntrySource(t, defs, "alpha")
	b := newEntrySource(t, defs, "beta")
	createView(t, defs, "tied", []string{b.Name, a.Name})
	var snapshot model.Snapshot
	snapshot.Data.Skills = []thvregistry.Skill{conformanceSkill("1")}
	for _, source := range []persistence.SourceDefinition{a, b} {
		if err := entries.ReplaceSnapshot(ctx, source, snapshot); err != nil {
			t.Fatal(err)
		}
	}
	if err := tiePositions(ctx, "tied"); err != nil {
		t.Fatal(err)
	}
	rows, err := diag.ListViewVersions(ctx, "tied")
	if err != nil || len(rows) != 2 || rows[0].SourceName != a.Name || rows[1].SourceName != b.Name ||
		rows[0].Position != 0 || rows[1].Position != 0 {
		t.Fatalf("equal historical positions must use source-name order: %+v / %v", rows, err)
	}
}

func diagnosticKey(v persistence.DiagnosticVersion) string {
	return v.Name + "\x00" + v.Version + "\x00" + string(v.Kind)
}
