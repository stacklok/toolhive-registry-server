package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/app/storage"
	storagemocks "github.com/stacklok/toolhive-registry-server/internal/app/storage/mocks"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestCatalogConfigFailureDoesNotMutateCaller(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"override", "validation", "consume"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			cfg := catalogTestConfig(t)
			cfg.Database.Password = "synthetic-app-secret"
			cfg.Database.MigrationPassword = "synthetic-migration-secret"
			cfg.Audit = &config.AuditConfig{EventTypes: []string{"source.read"}}
			original := cfg.Clone()
			fail := errors.New("blocked")
			ctrl := gomock.NewController(t)
			factory := storagemocks.NewMockCatalogFactory(ctrl)
			if stage == "consume" {
				store := storagemocks.NewMockCatalogStorage(ctrl)
				factory.EXPECT().Open(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, copied *config.Config) (storage.CatalogStorage, error) {
					require.True(t, copied.Database.Password == cfg.Database.Password && copied.Database.MigrationPassword == cfg.Database.MigrationPassword, "credentials must survive copying")
					return store, nil
				})
				store.EXPECT().Preflight(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
				store.EXPECT().Consume(gomock.Any(), gomock.Any()).Return(fail)
				store.EXPECT().Close()
			}
			_, err := NewCatalogApp(t.Context(), CatalogOptions{Config: cfg, Storage: factory, Overrides: []func(*config.Config) error{
				func(copied *config.Config) error {
					copied.Sources[0].Name = "changed"
					copied.Registries[0].Sources[0] = "changed"
					copied.Audit.EventTypes[0] = "source.create"
					if stage == "override" {
						return fail
					}
					if stage == "validation" {
						copied.Auth.Mode = "invalid"
					}
					return nil
				},
			}})
			require.Error(t, err)
			require.True(t, reflect.DeepEqual(cfg, original), "failed preparation must not mutate caller configuration")
		})
	}
}

func TestCatalogConfigCallerAndRetainedOverrideCannotChangeRuntime(t *testing.T) {
	t.Parallel()
	cfg := catalogTestConfig(t)
	conn, pool := catalogPG(t, cfg)
	issuer, token := catalogJWKS(t)
	cfg.Auth = &config.AuthConfig{Mode: config.AuthModeOAuth, InsecureAllowHTTP: true, OAuth: &config.OAuthConfig{Providers: []config.OAuthProviderConfig{{Name: "test", IssuerURL: issuer.URL, Audience: "catalog-test", JwksUrl: issuer.URL + "/jwks", AllowPrivateIP: true}}}}
	cfg.Authorization.AllowAnonymousCatalog = false
	cfg.Authorization.IdentityMappings = []config.IdentityMapping{{Issuer: issuer.URL, Groups: &config.AttributeMapping{Claim: "team", Format: "string-array"}}}
	// The JWKS helper emits a scalar team claim; space-delimited mapping preserves it.
	cfg.Authorization.IdentityMappings[0].Groups.Format = "space-delimited"
	require.NoError(t, os.WriteFile(cfg.Authorization.PolicyFile, []byte(`permit(principal, action == Registry::Action::"catalog.read", resource) when { principal has groups && principal.groups.contains("allowed") };`), 0600))
	approveCatalog(t, conn, cfg, true)
	var retained *config.Config
	app, err := NewCatalogApp(t.Context(), CatalogOptions{Config: cfg, Storage: storage.CatalogPostgres{BorrowedPool: pool},
		Overrides: []func(*config.Config) error{func(copied *config.Config) error { retained = copied; return nil }},
		HTTP:      []RegistryAppOptions{WithAddress("127.0.0.1:0"), WithInternalAddress("127.0.0.1:0")}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = app.Stop() })
	for _, external := range []*config.Config{cfg, retained} {
		external.Authorization.IdentityMappings[0].Groups.Claim = "unreviewed"
		external.Auth.OAuth.Providers[0].IssuerURL = "https://changed.invalid"
		external.Registries[0].Sources[0] = "changed"
		external.Sources[0].Managed = nil
	}
	require.NoError(t, app.Start())
	public, _ := app.Addresses()
	catalogRequest(t, public, http.MethodGet, "/registry/public/v0.1/servers", "", token("allowed"), 200)
	catalogRequest(t, public, http.MethodGet, "/registry/public/v0.1/servers", "", token("denied"), 403)
	require.NoError(t, app.Stop())
}
