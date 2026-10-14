// Package pages supplies direct ConsumerPages conformance for replacement applications.
// Unlike the sibling backend suite, it needs no definitions, snapshots or catalog reader.
package pages

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// Record is an independent expectation, in the application's exact list/history order.
// Latest identifies the expected latest detail, not the last record in host order.
type Record struct {
	Name, Version, Description string
	Latest                     bool
}

// Fixture supplies an initialized server-only replacement and synthetic host context.
// Records are initially visible; HiddenName exists but is inaccessible and
// MissingName does not exist. Hide removes
// a whole name from current selection; Revoke denies all operations in Context.
// Callbacks and contexts remain host-owned. Setup must return fresh isolated state
// per subtest, with at least two versions of the first name and a nonempty Search.
type Fixture struct {
	Pages                                 httpapi.ConsumerPages
	Context                               context.Context
	View, HiddenName, MissingName, Search string
	Records                               []Record
	Hide                                  func(string)
	Revoke                                func()
}

// Run exercises ready ConsumerPages directly, without unrelated backend contracts.
// It checks ordering, bounded continuation, search, exact/latest, current selection
// and admission between pages, generic not-found classification and cancellation.
//
//nolint:gocyclo // This table-driven conformance entry point owns all required scenarios.
func Run(t *testing.T, setup func(*testing.T) Fixture) {
	t.Helper()
	for _, scenario := range []string{"reads", "selection", "admission", "cancellation"} {
		t.Run(scenario, func(t *testing.T) {
			f := setup(t)
			if f.Pages == nil || f.Context == nil || len(f.Records) < 2 || f.View == "" ||
				f.HiddenName == "" || f.MissingName == "" || f.Search == "" ||
				f.Hide == nil || f.Revoke == nil {
				t.Fatal("incomplete ConsumerPages fixture")
			}
			name := catalog.EntryName{Kind: persistence.ServerKind, Name: f.Records[0].Name}
			query := catalog.Query{Kind: persistence.ServerKind, Limit: 1}
			first, err := f.Pages.List(f.Context, f.View, query)
			if err != nil || len(first.Entries) != 1 || first.NextCursor == "" {
				t.Fatalf("first list page: %+v / %v", first, err)
			}
			history, err := f.Pages.ListVersions(f.Context, f.View, name, catalog.PageRequest{Limit: 1})
			if err != nil || len(history.Entries) != 1 || history.NextCursor == "" {
				t.Fatalf("first history page: %+v / %v", history, err)
			}
			query.Cursor = first.NextCursor
			request := catalog.PageRequest{Limit: 1, Cursor: history.NextCursor}
			switch scenario {
			case "reads":
				query.Cursor = ""
				assertRecords(t, collect(t, f, query, nil), f.Records)
				var searched, named []Record
				for _, record := range f.Records {
					if strings.Contains(strings.ToLower(record.Name+" "+record.Description), strings.ToLower(f.Search)) {
						searched = append(searched, record)
					}
					if record.Name == name.Name {
						named = append(named, record)
					}
					recordName := catalog.EntryName{Kind: persistence.ServerKind, Name: record.Name}
					e, err := f.Pages.Get(f.Context, f.View, recordName, catalog.VersionSelector{Exact: record.Version})
					if err != nil {
						t.Fatal(err)
					}
					assertRecords(t, []Record{recordOf(t, e)}, []Record{record})
					if record.Latest {
						e, err = f.Pages.Get(f.Context, f.View, recordName, catalog.VersionSelector{Latest: true})
						if err != nil {
							t.Fatal(err)
						}
						assertRecords(t, []Record{recordOf(t, e)}, []Record{record})
					}
				}
				query.Search = f.Search
				assertRecords(t, collect(t, f, query, nil), searched)
				query.Search, query.Name = "", name.Name
				assertRecords(t, collect(t, f, query, nil), named)
				assertRecords(t, collect(t, f, catalog.Query{Limit: 1}, &name), named)
				for _, inaccessible := range []string{f.HiddenName, f.MissingName} {
					n := catalog.EntryName{Kind: persistence.ServerKind, Name: inaccessible}
					_, err := f.Pages.Get(f.Context, f.View, n, catalog.VersionSelector{Latest: true})
					if !errors.Is(err, persistence.ErrNotFound) {
						t.Fatalf("detail classification: %v", err)
					}
					_, err = f.Pages.ListVersions(f.Context, f.View, n, catalog.PageRequest{Limit: 1})
					if !errors.Is(err, persistence.ErrNotFound) {
						t.Fatalf("history classification: %v", err)
					}
				}
			case "selection":
				f.Hide(name.Name)
				var remaining []Record
				for _, record := range f.Records {
					if record.Name != name.Name {
						remaining = append(remaining, record)
					}
				}
				assertRecords(t, collect(t, f, query, nil), remaining)
				for _, selector := range []catalog.VersionSelector{{Exact: f.Records[0].Version}, {Latest: true}} {
					_, err = f.Pages.Get(f.Context, f.View, name, selector)
					if !errors.Is(err, persistence.ErrNotFound) {
						t.Fatalf("hidden detail: %v", err)
					}
				}
				_, err = f.Pages.ListVersions(f.Context, f.View, name, request)
				if !errors.Is(err, persistence.ErrNotFound) {
					t.Fatalf("hidden history continuation: %v", err)
				}
			case "admission":
				f.Revoke()
				_, listErr := f.Pages.List(f.Context, f.View, query)
				_, getErr := f.Pages.Get(f.Context, f.View, name, catalog.VersionSelector{Latest: true})
				_, historyErr := f.Pages.ListVersions(f.Context, f.View, name, request)
				if listErr == nil || getErr == nil || historyErr == nil {
					t.Fatal("revoked context still admitted")
				}
			case "cancellation":
				ctx, cancel := context.WithCancel(f.Context)
				cancel()
				_, listErr := f.Pages.List(ctx, f.View, query)
				_, getErr := f.Pages.Get(ctx, f.View, name, catalog.VersionSelector{Latest: true})
				_, historyErr := f.Pages.ListVersions(ctx, f.View, name, request)
				for _, err := range []error{listErr, getErr, historyErr} {
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("cancellation: %v", err)
					}
				}
			}
		})
	}
}

func collect(t *testing.T, f Fixture, q catalog.Query, name *catalog.EntryName) []Record {
	t.Helper()
	var records []Record
	for calls := 0; calls <= len(f.Records); calls++ {
		var page catalog.Page
		var err error
		if name == nil {
			page, err = f.Pages.List(f.Context, f.View, q)
		} else {
			page, err = f.Pages.ListVersions(f.Context, f.View, *name, catalog.PageRequest{Limit: q.Limit, Cursor: q.Cursor})
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Entries) > q.Limit {
			t.Fatal("unbounded page")
		}
		for _, entry := range page.Entries {
			records = append(records, recordOf(t, entry))
		}
		if page.NextCursor == "" {
			return records
		}
		if page.NextCursor == q.Cursor || len(page.Entries) == 0 {
			t.Fatal("non-progressing continuation")
		}
		q.Cursor = page.NextCursor
	}
	t.Fatal("continuation did not terminate")
	return nil
}

func recordOf(t *testing.T, e persistence.Entry) Record {
	t.Helper()
	if e.Kind != persistence.ServerKind || e.Server == nil || e.Name != e.Server.Name || e.Version != e.Server.Version {
		t.Fatalf("incoherent server entry: %+v", e)
	}
	return Record{Name: e.Name, Version: e.Version, Description: e.Server.Description, Latest: e.IsLatest}
}

func assertRecords(t *testing.T, got, want []Record) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("records: got %+v; want %+v", got, want)
	}
}
