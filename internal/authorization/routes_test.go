package authorization

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cedar-policy/cedar-go"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/auth"
	readermocks "github.com/stacklok/toolhive-registry-server/internal/authorization/mocks"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	adminmocks "github.com/stacklok/toolhive-registry-server/pkg/registry/catalog/mocks"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
)

func TestHTTPAdmissionDiagnosticFailsClosed(t *testing.T) {
	t.Parallel()
	// Deliberately bypass the loader only inside this package's test to simulate
	// Allow+diagnostic after evaluator failure. Never admitted by production Load.
	policies, err := cedar.NewPolicySetFromBytes("test", []byte(permitCatalog+`permit(principal,action,resource) when { principal.missing == true };`))
	require.NoError(t, err)
	gate, err := NewRegistrar(&Engine{policies: policies, revision: "test"}, nil, nil)
	require.NoError(t, err)
	reader := readermocks.NewMockReader(gomock.NewController(t))
	diagnostics, err := catalog.NewDiagnostics(adminmocks.NewMockEntryDiagnostics(gomock.NewController(t)), adminmocks.NewMockSourceStatusReader(gomock.NewController(t)))
	require.NoError(t, err)
	guarded, _, err := gate.GuardCatalog(reader, httpapi.AdminOperations{Sources: adminmocks.NewMockSources(gomock.NewController(t)), Views: adminmocks.NewMockViews(gomock.NewController(t)), Publisher: adminmocks.NewMockManagedPublisher(gomock.NewController(t)), Diagnostics: diagnostics})
	require.NoError(t, err)
	mw, _, err := auth.NewAdmissionAuthMiddleware(context.Background(), &config.AuthConfig{Mode: config.AuthModeAnonymous}, true, auth.DefaultValidatorFactory)
	require.NoError(t, err)
	router := chi.NewRouter()
	handler, err := gate.DecorateCatalog(httpapi.RouteDescriptor{Method: "GET", RegistryParam: "registryName", Operation: httpapi.List, Kind: "server"},
		"/{registryName}/servers", CatalogRead, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, err := guarded.List(r.Context(), "engineering", catalog.Query{Kind: "server"})
			require.ErrorIs(t, err, ErrEvaluation)
			// Even a handler that attempts to write its own error cannot overwrite the
			// fail-closed response or expose a diagnostic after the facade rejects it.
			http.Error(w, "SECRET diagnostic", 500)
		}), mw)
	require.NoError(t, err)
	router.Method("GET", "/{registryName}/servers", handler)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/engineering/servers", nil))
	require.Equal(t, 503, response.Code)
	require.NotContains(t, response.Body.String(), "SECRET")
	require.NotContains(t, response.Body.String(), "missing")
}
