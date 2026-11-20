package app

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	database "github.com/stacklok/toolhive-registry-server/internal/service/db"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// fakePages returns fixed results and records nothing.
type fakePages struct {
	page catalog.Page
	err  error
}

func (f fakePages) List(context.Context, string, catalog.Query) (catalog.Page, error) {
	return f.page, f.err
}

func (f fakePages) Get(context.Context, string, catalog.EntryName, catalog.VersionSelector) (persistence.Entry, error) {
	return persistence.Entry{}, f.err
}

func (f fakePages) ListVersions(context.Context, string, catalog.EntryName, catalog.PageRequest) (catalog.Page, error) {
	return f.page, f.err
}

// Consumer reads keep v1.5.4's span names, tracer and attributes.
func TestTracedPages_Spans(t *testing.T) {
	t.Parallel()
	page := catalog.Page{Entries: make([]persistence.Entry, 3)}
	server := catalog.EntryName{Kind: persistence.ServerKind, Name: "io.example/weather"}
	skill := catalog.EntryName{Kind: persistence.SkillKind, Namespace: "io.example", Name: "skill"}
	plugin := catalog.EntryName{Kind: persistence.PluginKind, Namespace: "io.example", Name: "plugin"}

	tests := []struct {
		name      string
		read      func(ctx context.Context, p catalog.Reader) error
		wantSpan  string
		wantAttrs []attribute.KeyValue
	}{
		{
			name: "list servers",
			read: func(ctx context.Context, p catalog.Reader) error {
				_, err := p.List(ctx, "main", catalog.Query{Kind: persistence.ServerKind, Limit: 30, Cursor: "c"})
				return err
			},
			wantSpan: "dbService.ListServers",
			wantAttrs: []attribute.KeyValue{
				attribute.Int("pagination.limit", 30), attribute.Bool("pagination.has_cursor", true),
				attribute.Int("result.count", 3),
			},
		},
		{
			name: "list server versions",
			read: func(ctx context.Context, p catalog.Reader) error {
				_, err := p.ListVersions(ctx, "main", server, catalog.PageRequest{Limit: 50})
				return err
			},
			wantSpan: "dbService.ListServerVersions",
			wantAttrs: []attribute.KeyValue{
				attribute.String("server.name", "io.example/weather"), attribute.Int("pagination.limit", 50),
				attribute.Int("result.count", 3),
			},
		},
		{
			name: "get server version",
			read: func(ctx context.Context, p catalog.Reader) error {
				_, err := p.Get(ctx, "main", server, catalog.VersionSelector{Latest: true})
				return err
			},
			wantSpan: "dbService.GetServerVersion",
			wantAttrs: []attribute.KeyValue{
				attribute.String("server.name", "io.example/weather"), attribute.String("server.version", "latest"),
			},
		},
		{
			name: "list skills",
			read: func(ctx context.Context, p catalog.Reader) error {
				_, err := p.List(ctx, "main", catalog.Query{Kind: persistence.SkillKind, Limit: 10})
				return err
			},
			wantSpan: "dbService.ListSkills",
		},
		{
			name: "list skill versions",
			read: func(ctx context.Context, p catalog.Reader) error {
				_, err := p.ListVersions(ctx, "main", skill, catalog.PageRequest{Limit: 10})
				return err
			},
			wantSpan: "dbService.ListSkills",
		},
		{
			name: "get skill version",
			read: func(ctx context.Context, p catalog.Reader) error {
				_, err := p.Get(ctx, "main", skill, catalog.VersionSelector{Exact: "1.0.0"})
				return err
			},
			wantSpan: "dbService.GetSkillVersion",
		},
		{
			name: "list plugins",
			read: func(ctx context.Context, p catalog.Reader) error {
				_, err := p.List(ctx, "main", catalog.Query{Kind: persistence.PluginKind, Limit: 10})
				return err
			},
			wantSpan: "dbService.ListPlugins",
		},
		{
			name: "get plugin version",
			read: func(ctx context.Context, p catalog.Reader) error {
				_, err := p.Get(ctx, "main", plugin, catalog.VersionSelector{Latest: true})
				return err
			},
			wantSpan: "dbService.GetPluginVersion",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			pages := newTracedPages(fakePages{page: page}, provider.Tracer(database.ServiceTracerName))
			require.NoError(t, tt.read(context.Background(), pages))

			spans := recorder.Ended()
			require.Len(t, spans, 1)
			span := spans[0]
			assert.Equal(t, tt.wantSpan, span.Name())
			assert.Equal(t, database.ServiceTracerName, span.InstrumentationScope().Name)
			attrs := span.Attributes()
			assert.Contains(t, attrs, attribute.String("registry.name", "main"))
			assert.Contains(t, attrs, attribute.String("db.system", "postgresql"))
			for _, want := range tt.wantAttrs {
				assert.Contains(t, attrs, want)
			}
			assert.Equal(t, codes.Unset, span.Status().Code)
		})
	}
}

func TestTracedPages_ErrorsAndNilTracer(t *testing.T) {
	t.Parallel()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	readErr := errors.New("query failed")
	pages := newTracedPages(fakePages{err: readErr}, provider.Tracer(database.ServiceTracerName))
	_, err := pages.List(context.Background(), "main", catalog.Query{Kind: persistence.ServerKind, Limit: 1})
	require.ErrorIs(t, err, readErr, "errors pass through unchanged")
	spans := recorder.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)

	raw := fakePages{}
	assert.Equal(t, catalog.Reader(raw), newTracedPages(raw, nil), "no tracer: no wrapper")
}
