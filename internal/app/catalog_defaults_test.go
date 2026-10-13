package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestDefaultCedarRetainsAuthenticationAdminAndExposureGates(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name             string
		admin, anonymous bool
	}{
		{"private admin off", false, false}, {"private admin on", true, false},
		{"anonymous catalog admin off", false, true}, {"anonymous catalog admin on", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := catalogTestConfig(t)
			cfg.Authorization.PolicyFile = ""
			cfg.Authorization.AllowAnonymousCatalog = tc.anonymous
			cfg.Admin.Enabled = tc.admin
			conn, pool := catalogPG(t, cfg)
			issuer, sign := catalogJWKS(t)
			cfg.Auth = &config.AuthConfig{Mode: config.AuthModeOAuth, InsecureAllowHTTP: true,
				OAuth: &config.OAuthConfig{Providers: []config.OAuthProviderConfig{{Name: "test", IssuerURL: issuer.URL,
					Audience: "catalog-test", JwksUrl: issuer.URL + "/jwks", AllowPrivateIP: true}}}}
			// Even allow-all cannot approve historical exposure or bypass preparation.
			_, err := NewCatalogApp(t.Context(), CatalogOptions{Config: cfg})
			require.Error(t, err)
			approveCatalog(t, conn, cfg, true)
			app := constructCatalog(t, cfg, pool, nil)
			require.NoError(t, app.Start())
			public, _ := app.Addresses()
			token := sign("any-authenticated-identity")
			catalogRequest(t, public, "GET", "/registry/public/v0.1/servers", "", token, 200)
			anonymousStatus := 401
			if tc.anonymous {
				anonymousStatus = 200
			}
			catalogRequest(t, public, "GET", "/registry/public/v0.1/servers", "", "", anonymousStatus)
			catalogRequest(t, public, "GET", "/registry/public/v0.1/servers", "", "invalid", 401)
			catalogRequest(t, public, "GET", "/openapi.json", "", token, 200)
			if tc.admin {
				catalogRequest(t, public, "GET", "/v1/sources", "", "", 401)
				catalogRequest(t, public, "GET", "/v1/sources", "", "invalid", 401)
				catalogRequest(t, public, "GET", "/v1/sources", "", token, 200)
				catalogRequest(t, public, "GET", "/v1/registries", "", token, 200)
				catalogRequest(t, public, "PUT", "/v1/registries/another", `{"sources":["managed"]}`, token, 201)
				catalogRequest(t, public, "DELETE", "/v1/registries/another", "", token, 204)
				catalogRequest(t, public, "POST", "/v1/entries", `{"skill":{"namespace":"com.example","name":"demo","version":"1","description":"default permit"}}`, token, 201)
				catalogRequest(t, public, "DELETE", "/v1/entries/skill/demo/versions/1", "", token, 204)
			} else {
				catalogRequest(t, public, "GET", "/v1/sources", "", token, 404)
			}
			require.NoError(t, app.Stop())
		})
	}
}
