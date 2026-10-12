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
	"github.com/stacklok/toolhive-registry-server/internal/config"
	mocksvc "github.com/stacklok/toolhive-registry-server/internal/service/mocks"
)

func TestHTTPAdmissionDiagnosticFailsClosed(t *testing.T) {
	t.Parallel()
	// Deliberately bypass the loader only inside this package's test to simulate
	// Allow+diagnostic after evaluator failure. Never admitted by production Load.
	policies, err := cedar.NewPolicySetFromBytes("test", []byte(permitCatalog+`permit(principal,action,resource) when { principal.missing == true };`))
	require.NoError(t, err)
	gate, err := NewRegistrar(&Engine{policies: policies, revision: "test"}, nil, nil)
	require.NoError(t, err)
	guarded, err := gate.Guard(mocksvc.NewMockRegistryService(gomock.NewController(t)))
	require.NoError(t, err)
	mw, _, err := auth.NewAdmissionAuthMiddleware(context.Background(), &config.AuthConfig{Mode: config.AuthModeAnonymous}, true, auth.DefaultValidatorFactory)
	require.NoError(t, err)
	router := chi.NewRouter()
	require.NoError(t, gate.Register(router, "GET", "/servers", CatalogRead, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := guarded.ListServers(r.Context())
		require.ErrorIs(t, err, ErrEvaluation)
		// Even a handler that attempts to write its own error cannot overwrite the
		// fail-closed response or expose a diagnostic after the facade rejects it.
		http.Error(w, "SECRET diagnostic", 500)
	}), mw))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/servers", nil))
	require.Equal(t, 503, response.Code)
	require.NotContains(t, response.Body.String(), "SECRET")
	require.NotContains(t, response.Body.String(), "missing")
}
