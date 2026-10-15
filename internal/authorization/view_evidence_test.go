package authorization

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestCatalogAdmissionEvidenceBindsCanonicalViewAndCapability(t *testing.T) {
	t.Parallel()
	for _, path := range []struct{ encoded, canonical string }{
		{"engineering", "engineering"}, {"eng%69neering", "engineering"},
		{"engineering%2Fteam", "engineering/team"}, {"engineering%252Fteam", "engineering/team"},
		{"engineering%25252Fteam", "engineering%2Fteam"},
		{"%E5%B7%A5%E7%A8%8B", "工程"},
	} {
		for _, mismatch := range []string{"view", "decode again", "operation", "kind", "diagnostics", "source"} {
			t.Run(path.encoded+"/"+mismatch, func(t *testing.T) {
				t.Parallel()
				ctrl := gomock.NewController(t)
				reader := readermocks.NewMockReader(ctrl)
				diagnostics, err := catalog.NewDiagnostics(adminmocks.NewMockEntryDiagnostics(ctrl), adminmocks.NewMockSourceStatusReader(ctrl))
				require.NoError(t, err)
				engine, err := Load("")
				require.NoError(t, err)
				gate, err := NewRegistrar(engine, nil, nil)
				require.NoError(t, err)
				pages, admin, err := gate.GuardCatalog(reader, httpapi.AdminOperations{Sources: adminmocks.NewMockSources(ctrl), Views: adminmocks.NewMockViews(ctrl), Publisher: adminmocks.NewMockManagedPublisher(ctrl), Diagnostics: diagnostics})
				require.NoError(t, err)
				mw, _, err := auth.NewAdmissionAuthMiddleware(t.Context(), &config.AuthConfig{Mode: config.AuthModeAnonymous}, true, auth.DefaultValidatorFactory)
				require.NoError(t, err)
				query := catalog.Query{Kind: persistence.ServerKind, Limit: 2, Cursor: "input-cursor"}
				page := catalog.Page{NextCursor: "output-cursor"}
				reader.EXPECT().List(gomock.Any(), path.canonical, query).Return(page, nil)
				h, err := gate.DecorateCatalog(httpapi.RouteDescriptor{Method: "GET", RegistryParam: "registryName", Operation: httpapi.List, Kind: persistence.ServerKind},
					"/{registryName}/servers", CatalogRead, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
						got, err := pages.List(r.Context(), path.canonical, query)
						require.NoError(t, err)
						require.Equal(t, page, got, "admission must preserve the supplied page and cursor")
						// The first permit must not authorize any of these subsequent calls.
						var denied error
						switch mismatch {
						case "view":
							_, denied = pages.List(r.Context(), "finance", query)
						case "decode again":
							other := strings.ReplaceAll(path.canonical, "%2F", "/")
							if other == path.canonical {
								other = "finance"
							}
							_, denied = pages.List(r.Context(), other, query)
						case "operation":
							_, denied = pages.Get(r.Context(), path.canonical, catalog.EntryName{Kind: persistence.ServerKind}, catalog.VersionSelector{Latest: true})
						case "kind":
							_, denied = pages.List(r.Context(), path.canonical, catalog.Query{Kind: persistence.SkillKind})
						case "diagnostics":
							_, denied = admin.Diagnostics.ListViewVersions(r.Context(), path.canonical)
						case "source":
							_, denied = admin.Sources.GetSource(r.Context(), path.canonical)
						}
						require.ErrorIs(t, denied, ErrEvidence)
						_, err = pages.List(r.Context(), path.canonical, query)
						require.ErrorIs(t, err, ErrEvidence, "a rejected request cannot resume backend work")
					}), mw)
				require.NoError(t, err)
				router := chi.NewRouter()
				router.Method("GET", "/{registryName}/servers", h)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest("GET", "/"+path.encoded+"/servers", nil))
				require.Equal(t, 500, response.Code)
			})
		}
	}
}

func TestCatalogResourceTypeCannotGrantOtherActions(t *testing.T) {
	t.Parallel()
	engine, err := Load("")
	require.NoError(t, err)
	p, err := principal(auth.VerifiedIdentity{Anonymous: true}, nil)
	require.NoError(t, err)
	require.ErrorIs(t, engine.evaluate(p, CatalogRead, instance), ErrEvidence)
	require.ErrorIs(t, engine.evaluate(p, ViewsRead, testResource(CatalogRead)), ErrEvidence)
	require.ErrorIs(t, (&Registrar{}).admitCatalog(context.Background(), "engineering", httpapi.List, persistence.ServerKind), ErrEvidence)
}
