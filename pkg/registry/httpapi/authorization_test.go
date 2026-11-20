package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
)

func TestProtectedDecoratorNilAuthorizerDeniesBothSurfaces(t *testing.T) {
	t.Parallel()
	for _, route := range []httpapi.RouteDescriptor{httpapi.ConsumerRoutes()[0], httpapi.AdminRoutes()[0]} {
		t.Run(route.ResourceType, func(t *testing.T) {
			t.Parallel()
			router := chi.NewRouter()
			router.Method(route.Method, route.Template, httpapi.Authorize(nil)(route,
				http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("nil authorizer reached application") })))
			path := "/sources"
			if route.RegistryParam != "" {
				path = "/public/v0.1/servers"
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(route.Method, path, nil))
			require.Equal(t, http.StatusForbidden, response.Code)
		})
	}
}
