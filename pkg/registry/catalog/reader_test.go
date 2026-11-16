package catalog_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// recordingReader is a supplied test backend, not a default selection fallback.
type recordingReader struct {
	calls    int
	view     string
	query    persistence.ViewQuery
	name     persistence.EntryName
	selector persistence.VersionSelector
	page     persistence.EntryPage
	entry    persistence.Entry
	err      error
}

func (b *recordingReader) ListViewEntries(_ context.Context, view string, q persistence.ViewQuery) (persistence.EntryPage, error) {
	b.calls++
	b.view = view
	b.query = q
	return b.page, b.err
}
func (b *recordingReader) GetViewEntry(_ context.Context, view string, name persistence.EntryName, selector persistence.VersionSelector) (persistence.Entry, error) {
	b.calls++
	b.view = view
	b.name = name
	b.selector = selector
	return b.entry, b.err
}

func TestReaderConstructionAndDelegation(t *testing.T) {
	t.Parallel()
	if _, err := catalog.NewReader(nil); !errors.Is(err, persistence.ErrInvalid) {
		t.Fatal(err)
	}
	backend := &recordingReader{page: persistence.EntryPage{Entries: []persistence.Entry{{Name: "selected", Version: "raw"}}, NextCursor: "host-selection"}, entry: persistence.Entry{Name: "selected"}}
	r, err := catalog.NewReader(backend)
	if err != nil || backend.calls != 0 {
		t.Fatalf("constructor IO: %v / %d", err, backend.calls)
	}
	q := catalog.Query{Kind: persistence.SkillKind, Name: "item", Namespace: "com.example", Search: "literal%", VersionSelector: catalog.VersionSelector{Exact: "latest"}, Limit: 2, Cursor: "opaque"}
	page, err := r.List(t.Context(), "consumer", q)
	if err != nil || !reflect.DeepEqual(page, backend.page) || backend.view != "consumer" || !reflect.DeepEqual(backend.query, q) || backend.calls != 1 {
		t.Fatalf("list changed selection: %+v / %+v / %v", page, backend, err)
	}
	name := catalog.EntryName{Kind: persistence.SkillKind, Name: "item", Namespace: "com.example"}
	selector := catalog.VersionSelector{Exact: "latest"}
	entry, err := r.Get(t.Context(), "consumer", name, selector)
	if err != nil || !reflect.DeepEqual(entry, backend.entry) || backend.name != name || backend.selector != selector || backend.calls != 2 {
		t.Fatalf("get changed selection: %+v / %+v / %v", entry, backend, err)
	}
	page, err = r.ListVersions(t.Context(), "consumer", name, catalog.PageRequest{Limit: 30, Cursor: "history-token"})
	want := catalog.Query{Kind: name.Kind, Name: name.Name, Namespace: name.Namespace, Limit: 30, Cursor: "history-token"}
	if err != nil || !reflect.DeepEqual(page, backend.page) || !reflect.DeepEqual(backend.query, want) || backend.calls != 3 {
		t.Fatalf("history changed selection: %+v / %+v / %v", page, backend, err)
	}
}

func TestReaderValidationAndErrors(t *testing.T) {
	t.Parallel()
	for _, err := range []error{persistence.ErrUnavailable, persistence.ErrInvalid, persistence.ErrNotFound, context.Canceled, context.DeadlineExceeded} {
		t.Run(err.Error(), func(t *testing.T) {
			t.Parallel()
			b := &recordingReader{err: err}
			r, e := catalog.NewReader(b)
			if e != nil {
				t.Fatal(e)
			}
			name := catalog.EntryName{Kind: persistence.PluginKind, Name: "item"}
			for _, operation := range []func() error{
				func() error { _, e := r.List(t.Context(), "view", catalog.Query{Kind: name.Kind, Limit: 1}); return e },
				func() error {
					_, e := r.Get(t.Context(), "view", name, catalog.VersionSelector{Latest: true})
					return e
				},
				func() error {
					_, e := r.ListVersions(t.Context(), "view", name, catalog.PageRequest{Limit: 1})
					return e
				},
			} {
				if e := operation(); !errors.Is(e, err) {
					t.Fatalf("error category lost: %v", e)
				}
			}
		})
	}
	b := &recordingReader{}
	r, e := catalog.NewReader(b)
	if e != nil {
		t.Fatal(e)
	}
	for _, operation := range []func() error{
		func() error {
			_, e := r.List(t.Context(), "", catalog.Query{Kind: persistence.SkillKind, Limit: 1})
			return e
		},
		func() error {
			_, e := r.List(t.Context(), "view", catalog.Query{Kind: persistence.SkillKind})
			return e
		},
		func() error {
			_, e := r.Get(t.Context(), "view", catalog.EntryName{Kind: persistence.SkillKind}, catalog.VersionSelector{Latest: true})
			return e
		},
		func() error {
			_, e := r.Get(t.Context(), "view", catalog.EntryName{Kind: persistence.SkillKind, Name: "item"}, catalog.VersionSelector{})
			return e
		},
		func() error {
			_, e := r.Get(t.Context(), "view", catalog.EntryName{Kind: persistence.SkillKind, Name: "item"}, catalog.VersionSelector{Latest: true, Exact: "1"})
			return e
		},
		func() error {
			_, e := r.ListVersions(t.Context(), "view", catalog.EntryName{Kind: persistence.SkillKind}, catalog.PageRequest{Limit: 1})
			return e
		},
	} {
		if e := operation(); !errors.Is(e, persistence.ErrInvalid) {
			t.Fatalf("invalid input: %v", e)
		}
	}
	if b.calls != 0 {
		t.Fatalf("invalid input reached storage: %d", b.calls)
	}
}
