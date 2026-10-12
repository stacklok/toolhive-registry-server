package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/audit"
	"github.com/stacklok/toolhive-registry-server/internal/authorization"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	mocksvc "github.com/stacklok/toolhive-registry-server/internal/service/mocks"
)

func TestAdmissionAuditSeparatesWriteOutcome(t *testing.T) {
	t.Parallel()
	mw, cfg := admissionTestAuth(t)
	path := filepath.Join(t.TempDir(), "audit.log")
	logger, err := audit.NewLogger(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, logger.Close()) })
	policyPath := filepath.Join(t.TempDir(), "policy.cedar")
	require.NoError(t, os.WriteFile(policyPath, []byte("// SECRET-POLICY\n"+admissionTestPermit), 0600))
	engine, err := authorization.Load(policyPath)
	require.NoError(t, err)
	gate, err := authorization.NewRegistrar(engine, nil, logger)
	require.NoError(t, err)
	raw := mocksvc.NewMockRegistryService(gomock.NewController(t))
	raw.EXPECT().CreateSource(gomock.Any(), "test", gomock.Any()).Return(nil, service.ErrClaimsInsufficient)
	auditCfg := &config.AuditConfig{Enabled: true}
	authenticate := func(next http.Handler) http.Handler { return mw(audit.Middleware(auditCfg, logger)(next)) }
	router, err := NewAdmissionServer(raw, gate, true, authenticate, WithAuthConfig(cfg), WithMiddlewares(audit.AuthFailureMiddleware(auditCfg, logger)))
	require.NoError(t, err)
	request := httptest.NewRequest("PUT", "/v1/sources/test", strings.NewReader(`{"claims":{"private":"SECRET-CLAIM"}}`))
	request.Header.Set("Authorization", "Bearer valid")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, 403, response.Code)
	request = httptest.NewRequest("GET", "/registry/default/v0.1/servers", nil)
	request.Header.Set("Authorization", "Bearer SECRET-TOKEN")
	router.ServeHTTP(httptest.NewRecorder(), request)
	for _, route := range []struct{ method, path string }{
		{"GET", "/v1/sources"},
		{"GET", "/v1/me"},
		{"GET", "/v1/entries/server/test/claims"},
		{"PUT", "/v1/entries/server/test/claims"},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(route.method, route.path, nil))
		require.Equal(t, http.StatusUnauthorized, response.Code)
		require.NotEmpty(t, response.Header().Get("WWW-Authenticate"))
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	output := string(data)
	require.Equal(t, 1, strings.Count(output, "authorization.admission"))
	require.Equal(t, 1, strings.Count(output, "source.update"))
	require.Equal(t, 5, strings.Count(output, "auth.unauthenticated"))
	require.Contains(t, output, "authorization.admission")
	require.Contains(t, output, "policy_revision")
	require.Contains(t, output, "authenticated:")
	require.Contains(t, output, "source.update")
	require.Contains(t, output, "auth.unauthenticated")
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "authorization.admission") {
			require.Contains(t, line, `"decision":"allow"`)
		}
		if strings.Contains(line, "source.update") {
			require.Contains(t, line, "denied")
			require.NotContains(t, line, `"outcome":"success"`)
		}
	}
	for _, secret := range []string{"SECRET-POLICY", "SECRET-CLAIM", "SECRET-TOKEN"} {
		require.NotContains(t, output, secret)
	}
}
