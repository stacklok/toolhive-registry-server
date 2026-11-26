package app

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/stacklok/toolhive-registry-server/internal/otel"
	database "github.com/stacklok/toolhive-registry-server/internal/service/db"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// tracedPages records v1.5.4's consumer-read spans (tracer
// database.ServiceTracerName, spans dbService.ListServers and so on) around
// the library's consumer reader, so existing trace queries keep working.
type tracedPages struct {
	catalog.Reader
	tracer trace.Tracer
}

// newTracedPages wraps pages; a nil tracer returns pages unchanged.
func newTracedPages(pages catalog.Reader, tracer trace.Tracer) catalog.Reader {
	if tracer == nil {
		return pages
	}
	return &tracedPages{Reader: pages, tracer: tracer}
}

// List traces a list of the latest entries: dbService.ListServers,
// ListSkills or ListPlugins.
func (p *tracedPages) List(ctx context.Context, view string, query catalog.Query) (catalog.Page, error) {
	var attrs []attribute.KeyValue
	if query.Kind == persistence.ServerKind {
		attrs = []attribute.KeyValue{otel.AttrPageSize.Int(query.Limit), otel.AttrHasCursor.Bool(query.Cursor != "")}
	}
	ctx, span := p.start(ctx, listSpan(query.Kind), view, attrs...)
	defer span.End()
	page, err := p.Reader.List(ctx, view, query)
	p.finishList(span, query.Kind, page, err)
	return page, err
}

// Get traces a single version: dbService.GetServerVersion, GetSkillVersion or
// GetPluginVersion.
func (p *tracedPages) Get(
	ctx context.Context, view string, name catalog.EntryName, selector catalog.VersionSelector,
) (persistence.Entry, error) {
	var attrs []attribute.KeyValue
	if name.Kind == persistence.ServerKind {
		version := selector.Exact
		if selector.Latest {
			version = "latest"
		}
		attrs = []attribute.KeyValue{otel.AttrServerName.String(name.Name), otel.AttrServerVersion.String(version)}
	}
	ctx, span := p.start(ctx, getSpan(name.Kind), view, attrs...)
	defer span.End()
	entry, err := p.Reader.Get(ctx, view, name, selector)
	otel.RecordError(span, err)
	return entry, err
}

// ListVersions traces one name's versions: dbService.ListServerVersions, or
// ListSkills / ListPlugins, which served skill and plugin versions in v1.5.4.
func (p *tracedPages) ListVersions(
	ctx context.Context, view string, name catalog.EntryName, request catalog.PageRequest,
) (catalog.Page, error) {
	spanName := listSpan(name.Kind)
	var attrs []attribute.KeyValue
	if name.Kind == persistence.ServerKind {
		spanName = "dbService.ListServerVersions"
		attrs = []attribute.KeyValue{otel.AttrServerName.String(name.Name), otel.AttrPageSize.Int(request.Limit)}
	}
	ctx, span := p.start(ctx, spanName, view, attrs...)
	defer span.End()
	page, err := p.Reader.ListVersions(ctx, view, name, request)
	p.finishList(span, name.Kind, page, err)
	return page, err
}

func (p *tracedPages) start(
	ctx context.Context, name, view string, attrs ...attribute.KeyValue,
) (context.Context, trace.Span) {
	attrs = append([]attribute.KeyValue{database.DBSystemPostgres, otel.AttrRegistryName.String(view)}, attrs...)
	return p.tracer.Start(ctx, name, trace.WithAttributes(attrs...))
}

// finishList records the error, or the result count for servers as v1.5.4 did.
func (*tracedPages) finishList(span trace.Span, kind persistence.EntryKind, page catalog.Page, err error) {
	if err != nil {
		otel.RecordError(span, err)
		return
	}
	if kind == persistence.ServerKind {
		span.SetAttributes(otel.AttrResultCount.Int(len(page.Entries)))
	}
}

func listSpan(kind persistence.EntryKind) string {
	switch kind {
	case persistence.SkillKind:
		return "dbService.ListSkills"
	case persistence.PluginKind:
		return "dbService.ListPlugins"
	case persistence.ServerKind:
		return "dbService.ListServers"
	default:
		return "dbService.ListServers"
	}
}

func getSpan(kind persistence.EntryKind) string {
	switch kind {
	case persistence.SkillKind:
		return "dbService.GetSkillVersion"
	case persistence.PluginKind:
		return "dbService.GetPluginVersion"
	case persistence.ServerKind:
		return "dbService.GetServerVersion"
	default:
		return "dbService.GetServerVersion"
	}
}
