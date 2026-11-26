package app

import (
	"context"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// emptyPages is a consumer reader with no registries.
type emptyPages struct{}

func (emptyPages) List(context.Context, string, catalog.Query) (catalog.Page, error) {
	return catalog.Page{}, persistence.ErrNotFound
}

func (emptyPages) Get(context.Context, string, catalog.EntryName, catalog.VersionSelector) (persistence.Entry, error) {
	return persistence.Entry{}, persistence.ErrNotFound
}

func (emptyPages) ListVersions(context.Context, string, catalog.EntryName, catalog.PageRequest) (catalog.Page, error) {
	return catalog.Page{}, persistence.ErrNotFound
}
