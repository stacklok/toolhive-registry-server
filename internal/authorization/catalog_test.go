package authorization

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/auth"
	readermocks "github.com/stacklok/toolhive-registry-server/internal/authorization/mocks"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	adminmocks "github.com/stacklok/toolhive-registry-server/pkg/registry/catalog/mocks"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

func TestCatalogEveryMethodFailsClosedWithoutOwnActionEvidence(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	// No backend expectations: even a broad valid permit must not admit omitted
	// middleware, an alternate handler or an operation mislabeled as catalog.read.
	reader := readermocks.NewMockReader(ctrl)
	sources := adminmocks.NewMockSources(ctrl)
	views := adminmocks.NewMockViews(ctrl)
	publisher := adminmocks.NewMockManagedPublisher(ctrl)
	entries := adminmocks.NewMockEntryDiagnostics(ctrl)
	status := adminmocks.NewMockSourceStatusReader(ctrl)
	diagnostics, err := catalog.NewDiagnostics(entries, status)
	require.NoError(t, err)
	engine, err := Load("")
	require.NoError(t, err)
	gate, err := NewRegistrar(engine, nil, nil)
	require.NoError(t, err)
	pages, admin, err := gate.GuardCatalog(reader, httpapi.AdminOperations{Sources: sources, Views: views, Publisher: publisher, Diagnostics: diagnostics})
	require.NoError(t, err)
	calls := []struct {
		name   string
		action Action
		call   func(context.Context) error
	}{
		{"List", CatalogRead, func(ctx context.Context) error { _, err := pages.List(ctx, "", catalog.Query{}); return err }},
		{"Get", CatalogRead, func(ctx context.Context) error {
			_, err := pages.Get(ctx, "", catalog.EntryName{}, catalog.VersionSelector{})
			return err
		}},
		{"ListVersions", CatalogRead, func(ctx context.Context) error {
			_, err := pages.ListVersions(ctx, "", catalog.EntryName{}, catalog.PageRequest{})
			return err
		}},
		{"CreateSource", SourcesWrite, func(ctx context.Context) error {
			_, err := admin.Sources.CreateSource(ctx, persistence.SourceDefinition{})
			return err
		}},
		{"UpdateSource", SourcesWrite, func(ctx context.Context) error {
			_, err := admin.Sources.UpdateSource(ctx, "", persistence.SourceDefinition{})
			return err
		}},
		{"GetSource", SourcesRead, func(ctx context.Context) error { _, err := admin.Sources.GetSource(ctx, ""); return err }},
		{"ListSources", SourcesRead, func(ctx context.Context) error { _, err := admin.Sources.ListSources(ctx); return err }},
		{"DeleteSource", SourcesDelete, func(ctx context.Context) error { return admin.Sources.DeleteSource(ctx, "") }},
		{"CreateView", ViewsWrite, func(ctx context.Context) error {
			_, err := admin.Views.CreateView(ctx, persistence.ViewDefinition{})
			return err
		}},
		{"UpdateView", ViewsWrite, func(ctx context.Context) error {
			_, err := admin.Views.UpdateView(ctx, "", persistence.ViewDefinition{})
			return err
		}},
		{"GetView", ViewsRead, func(ctx context.Context) error { _, err := admin.Views.GetView(ctx, ""); return err }},
		{"ListViews", ViewsRead, func(ctx context.Context) error { _, err := admin.Views.ListViews(ctx); return err }},
		{"DeleteView", ViewsDelete, func(ctx context.Context) error { return admin.Views.DeleteView(ctx, "") }},
		{"Publish", EntriesPublish, func(ctx context.Context) error {
			_, err := admin.Publisher.Publish(ctx, persistence.Entry{})
			return err
		}},
		{"DeleteManaged", EntriesDelete, func(ctx context.Context) error { return admin.Publisher.DeleteManaged(ctx, "", "", "") }},
		{"ListSourceVersions", SourcesRead, func(ctx context.Context) error { _, err := admin.Diagnostics.ListSourceVersions(ctx, ""); return err }},
		{"ListViewVersions", ViewsRead, func(ctx context.Context) error { _, err := admin.Diagnostics.ListViewVersions(ctx, ""); return err }},
		{"Status", SourcesRead, func(ctx context.Context) error { _, err := admin.Diagnostics.Status(ctx, ""); return err }},
	}
	for _, tc := range calls {
		require.ErrorIs(t, tc.call(t.Context()), ErrEvidence, tc.name)
	}
	mw, _, err := auth.NewAdmissionAuthMiddleware(t.Context(), &config.AuthConfig{Mode: config.AuthModeAnonymous}, true, auth.DefaultValidatorFactory)
	require.NoError(t, err)
	router := chi.NewRouter()
	// Private route evidence can be generated only after explicit identity evidence.
	require.NoError(t, gate.Register(router, "GET", "/alternate", CatalogRead, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, tc := range calls {
			if tc.action != CatalogRead {
				require.ErrorIs(t, tc.call(r.Context()), ErrEvidence, tc.name)
			}
		}
		http.Error(w, "must not overwrite rejection", 200)
	}), mw))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/alternate", nil))
	require.Equal(t, 500, response.Code)
	require.NotContains(t, response.Body.String(), "overwrite")
	require.NoError(t, gate.Register(router, "GET", "/wrong-consumer", MetadataRead, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		for _, tc := range calls {
			if tc.action == CatalogRead {
				require.ErrorIs(t, tc.call(r.Context()), ErrEvidence, tc.name)
			}
		}
	}), mw))
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/wrong-consumer", nil))
	require.Equal(t, 500, response.Code)
	omitted := chi.NewRouter()
	require.NoError(t, gate.Register(omitted, "GET", "/omitted", CatalogRead, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("handler called without verified identity") }), func(h http.Handler) http.Handler { return h }))
	response = httptest.NewRecorder()
	omitted.ServeHTTP(response, httptest.NewRequest("GET", "/omitted", nil))
	require.Equal(t, 500, response.Code)
}
