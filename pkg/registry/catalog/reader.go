// Package catalog provides non-personalized consumer reads over host-supplied
// persistence. It performs no authorization, opens no storage and starts no work.
package catalog

import (
	"context"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// Query reuses the persistence view selection model.
type Query = persistence.ViewQuery

// EntryName reuses the persistence name and namespace filter.
type EntryName = persistence.EntryName

// VersionSelector distinguishes latest selection from an exact raw version.
type VersionSelector = persistence.VersionSelector

// Page reuses the selected persistence payloads and continuation metadata.
type Page = persistence.EntryPage

// PageRequest supplies a finite bound and an optional backend-private cursor.
type PageRequest struct {
	Limit  int
	Cursor string
}

// Reader is the replaceable application contract, not the legacy RegistryService.
// Hosts own admission and any personalized selection outside this default reader.
type Reader interface {
	List(context.Context, string, Query) (Page, error)
	Get(context.Context, string, EntryName, VersionSelector) (persistence.Entry, error)
	ListVersions(context.Context, string, EntryName, PageRequest) (Page, error)
}

type reader struct{ entries persistence.ViewEntryReader }

// NewReader requires an initialized, non-nil backend and performs no IO.
// Selection and pagination are delegated completely to the supplied reader,
// not composed from source pages.
func NewReader(entries persistence.ViewEntryReader) (Reader, error) {
	if entries == nil {
		return nil, persistence.ErrInvalid
	}
	return &reader{entries: entries}, nil
}

func (r *reader) List(ctx context.Context, view string, query Query) (Page, error) {
	if err := query.Validate(view); err != nil {
		return Page{}, err
	}
	return r.entries.ListViewEntries(ctx, view, query)
}

func (r *reader) Get(ctx context.Context, view string, name EntryName, selector VersionSelector) (persistence.Entry, error) {
	if err := name.Validate(view, selector); err != nil {
		return persistence.Entry{}, err
	}
	return r.entries.GetViewEntry(ctx, view, name, selector)
}

func (r *reader) ListVersions(ctx context.Context, view string, name EntryName, page PageRequest) (Page, error) {
	if name.Name == "" {
		return Page{}, persistence.ErrInvalid
	}
	return r.List(ctx, view, Query{
		Kind: name.Kind, Name: name.Name, Namespace: name.Namespace, Limit: page.Limit, Cursor: page.Cursor,
	})
}
