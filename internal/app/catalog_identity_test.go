package app

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

//nolint:paralleltest // Global slog capture must not overlap parallel tests.
func TestCatalogAuthenticationAccessAndRouteAuditProjectOnlyVerifiedActor(t *testing.T) {
	var logs catalogLogBuffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	cfg := catalogTestConfig(t)
	conn, pool := catalogPG(t, cfg)
	issuer, sign := catalogJWKS(t)
	cfg.Auth = &config.AuthConfig{Mode: config.AuthModeOAuth, InsecureAllowHTTP: true, OAuth: &config.OAuthConfig{Providers: []config.OAuthProviderConfig{{Name: "test", IssuerURL: issuer.URL, Audience: "catalog-test", JwksUrl: issuer.URL + "/jwks", AllowPrivateIP: true}}}}
	cfg.Admin.Enabled = true
	cfg.Authorization.AllowAnonymousCatalog = false
	cfg.Audit = &config.AuditConfig{Enabled: true, LogFile: filepath.Join(t.TempDir(), "audit.json"), IncludeRequestData: false}
	require.NoError(t, os.WriteFile(cfg.Authorization.PolicyFile, []byte(`permit(principal, action in [Registry::Action::"catalog.read", Registry::Action::"sources.read"], resource) when { principal.authenticated };`), 0600))
	approveCatalog(t, conn, cfg, true)
	app := constructCatalog(t, cfg, pool, nil)
	require.NoError(t, app.Start())
	public, _ := app.Addresses()
	const raw = "PRIVATE-identity-marker"
	token := sign(raw)
	actor := auth.PrincipalID(auth.VerifiedIdentity{Issuer: issuer.URL, Subject: raw, Claims: jwt.MapClaims{"iss": issuer.URL, "sub": raw}})
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/registry/public/v0.1/servers", 200},
		{"/v1/sources", 200},
		{"/v1/registries", 403},
		{"/v1/me", 410},
	} {
		catalogRequest(t, public, "GET", tc.path, "", token, tc.status)
	}
	request, err := http.NewRequestWithContext(t.Context(), "GET", "http://"+public+"/v1/sources", nil)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer PRIVATE-invalid-token")
	request.Header.Set("X-Subject", raw)
	request.Header.Set("X-Actor", actor)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, 401, response.StatusCode)
	require.NoError(t, app.Stop())
	auditData, err := os.ReadFile(cfg.Audit.LogFile)
	require.NoError(t, err)
	access := logs.String()
	for _, data := range []string{access, string(auditData)} {
		for _, forbidden := range []string{raw, raw + "-display", raw + "-username", raw + "@private.invalid", token, "PRIVATE-invalid-token"} {
			require.False(t, strings.Contains(data, forbidden), "ordinary logs must not contain raw identity or credentials")
		}
		require.Contains(t, data, actor)
	}
	require.Contains(t, access, "Authentication successful")
	require.Contains(t, access, "HTTP request")
	for _, code := range []string{`"status":200`, `"status":403`, `"status":410`, `"status":401`} {
		require.Contains(t, access, code)
	}
	for _, marker := range []string{"sources.read", "views.read", "authorization.admission", "success", "denied", "auth.unauthenticated"} {
		require.Contains(t, string(auditData), marker)
	}
	for _, line := range strings.Split(access, "\n") {
		if strings.Contains(line, `"status":401`) {
			require.NotContains(t, line, actor, "unverified actor headers must not establish attribution")
		}
	}
}

type catalogLogBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *catalogLogBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}
func (b *catalogLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}
