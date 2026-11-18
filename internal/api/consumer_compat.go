package api

import (
	"context"
	"strings"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// ManagedSourceIDs reports the IDs of managed sources. The app injects it so
// this layer never queries storage itself.
type ManagedSourceIDs func(ctx context.Context) (map[string]bool, error)

// compatPages keeps the consumer API's v1.5.4 response shapes on top of the
// library's payloads, which round-trip exactly what was published:
//   - skill and plugin status is upper case ("ACTIVE"), as stored;
//   - every server carries "_meta", {} when the publisher provided none;
//   - skills and plugins in the managed source always carry "repository"
//     ({} when unset), because v1.5.4 stored an absent repository as JSON
//     null and returned it as an empty object.
type compatPages struct {
	next    httpapi.ConsumerPages
	managed ManagedSourceIDs
}

var _ httpapi.ConsumerPages = compatPages{}

func (p compatPages) List(ctx context.Context, view string, q catalog.Query) (catalog.Page, error) {
	page, err := p.next.List(ctx, view, q)
	if err != nil {
		return page, err
	}
	return page, p.shapeAll(ctx, page.Entries)
}

func (p compatPages) Get(
	ctx context.Context, view string, name catalog.EntryName, selector catalog.VersionSelector,
) (persistence.Entry, error) {
	entry, err := p.next.Get(ctx, view, name, selector)
	if err != nil {
		return entry, err
	}
	entries := []persistence.Entry{entry}
	return entries[0], p.shapeAll(ctx, entries)
}

func (p compatPages) ListVersions(
	ctx context.Context, view string, name catalog.EntryName, req catalog.PageRequest,
) (catalog.Page, error) {
	page, err := p.next.ListVersions(ctx, view, name, req)
	if err != nil {
		return page, err
	}
	return page, p.shapeAll(ctx, page.Entries)
}

func (p compatPages) shapeAll(ctx context.Context, entries []persistence.Entry) error {
	var managed map[string]bool
	for i := range entries {
		e := &entries[i]
		switch {
		case e.Server != nil:
			if e.Server.Meta == nil {
				e.Server.Meta = &upstreamv0.ServerMeta{}
			}
		case e.Skill != nil, e.Plugin != nil:
			if managed == nil && p.managed != nil {
				ids, err := p.managed(ctx)
				if err != nil {
					return err
				}
				managed = ids
			}
			shapeExtension(e, managed[e.SourceID])
		}
	}
	return nil
}

func shapeExtension(e *persistence.Entry, inManagedSource bool) {
	if e.Skill != nil {
		e.Skill.Status = strings.ToUpper(e.Skill.Status)
		if inManagedSource && e.Skill.Repository == nil {
			e.Skill.Repository = &model.SkillRepository{}
		}
	}
	if e.Plugin != nil {
		e.Plugin.Status = strings.ToUpper(e.Plugin.Status)
		if inManagedSource && e.Plugin.Repository == nil {
			e.Plugin.Repository = &model.PluginRepository{}
		}
	}
}
