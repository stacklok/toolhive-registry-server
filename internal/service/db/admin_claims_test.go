package database

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	database "github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// setupTestServiceWithCodecs creates a test service whose connection pool
// has custom enum array codecs registered (needed for CreateSource etc.).
func setupTestServiceWithCodecs(t *testing.T) (*dbService, func()) {
	t.Helper()

	ctx := context.Background()
	db, cleanupFunc := database.SetupTestDB(t)

	connStr := db.Config().ConnString()

	poolCfg, err := pgxpool.ParseConfig(connStr)
	require.NoError(t, err)

	poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		enumTypes := []string{"sync_status", "icon_theme", "creation_type"}
		for _, enumName := range enumTypes {
			var enumOID uint32
			if err := conn.QueryRow(ctx, "SELECT oid FROM pg_type WHERE typname = $1", enumName).Scan(&enumOID); err != nil {
				return fmt.Errorf("failed to get %s OID: %w", enumName, err)
			}
			var arrayOID uint32
			if err := conn.QueryRow(ctx, "SELECT oid FROM pg_type WHERE typname = $1", "_"+enumName).Scan(&arrayOID); err != nil {
				return fmt.Errorf("failed to get %s[] OID: %w", enumName, err)
			}
			conn.TypeMap().RegisterType(&pgtype.Type{
				Name: enumName + "[]",
				OID:  arrayOID,
				Codec: &pgtype.ArrayCodec{
					ElementType: &pgtype.Type{
						Name:  enumName,
						OID:   enumOID,
						Codec: pgtype.TextCodec{},
					},
				},
			})
		}
		return nil
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	require.NoError(t, err)

	svcCleanup := func() {
		pool.Close()
		cleanupFunc()
	}

	svc := &dbService{
		pool:        pool,
		maxMetaSize: config.DefaultMaxMetaSize,
	}
	return svc, svcCleanup
}

// managedSourceReq returns a SourceCreateRequest for a managed source with the given claims.
func managedSourceReq(claims map[string]any) *service.SourceCreateRequest {
	return &service.SourceCreateRequest{
		Managed: &config.ManagedConfig{},
		Claims:  claims,
	}
}

// ---------------------------------------------------------------------------
// Source CRUD — claim-scoped tests
// ---------------------------------------------------------------------------

func TestCreateSource_ClaimsWithinJWT(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	ctx := auth.ContextWithClaims(t.Context(), jwt.MapClaims{"org": "acme", "team": "eng"})

	src, err := svc.CreateSource(ctx, "cs-within-jwt", managedSourceReq(map[string]any{"org": "acme"}))
	require.NoError(t, err)
	require.NotNil(t, src)
	assert.Equal(t, "cs-within-jwt", src.Name)
}

func TestCreateSource_ClaimsExceedJWT(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	// JWT only has org=acme, but request also demands team=eng
	ctx := auth.ContextWithClaims(t.Context(), jwt.MapClaims{"org": "acme"})

	src, err := svc.CreateSource(ctx, "cs-exceed-jwt", managedSourceReq(map[string]any{"org": "acme", "team": "eng"}))
	require.Error(t, err)
	assert.True(t, errors.Is(err, service.ErrClaimsInsufficient), "expected ErrClaimsInsufficient, got %v", err)
	assert.Nil(t, src)
}

func TestCreateSource_NilJWT_SkipsCheck(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	// No JWT claims in context (anonymous)
	ctx := t.Context()

	src, err := svc.CreateSource(ctx, "cs-nil-jwt", managedSourceReq(map[string]any{"org": "acme"}))
	require.NoError(t, err)
	require.NotNil(t, src)
	assert.Equal(t, "cs-nil-jwt", src.Name)
}

func TestUpdateSource_CallerCoversExistingClaims(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	// Create source with claims
	ctx := auth.ContextWithClaims(t.Context(), jwt.MapClaims{"org": "acme", "team": "eng"})
	_, err := svc.CreateSource(ctx, "us-covers", managedSourceReq(map[string]any{"org": "acme"}))
	require.NoError(t, err)

	// Update with a caller whose JWT covers both existing and new claims
	updated, err := svc.UpdateSource(ctx, "us-covers", managedSourceReq(map[string]any{"org": "acme", "team": "eng"}))
	require.NoError(t, err)
	require.NotNil(t, updated)
}

func TestUpdateSource_CallerDoesNotCoverExistingClaims(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	// Create source with org=acme (anonymous context to bypass check)
	createCtx := t.Context()
	_, err := svc.CreateSource(createCtx, "us-no-cover", managedSourceReq(map[string]any{"org": "acme"}))
	require.NoError(t, err)

	// Update with a caller whose JWT does NOT cover existing claims
	updateCtx := auth.ContextWithClaims(t.Context(), jwt.MapClaims{"org": "contoso"})
	updated, err := svc.UpdateSource(updateCtx, "us-no-cover", managedSourceReq(map[string]any{"org": "contoso"}))
	require.Error(t, err)
	assert.True(t, errors.Is(err, service.ErrClaimsInsufficient), "expected ErrClaimsInsufficient, got %v", err)
	assert.Nil(t, updated)
}

func TestDeleteSource_CallerCoversClaims(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	// Create source with claims (anonymous)
	createCtx := t.Context()
	_, err := svc.CreateSource(createCtx, "ds-covers", managedSourceReq(map[string]any{"org": "acme"}))
	require.NoError(t, err)

	// Delete with a caller whose JWT covers the source claims
	deleteCtx := auth.ContextWithClaims(t.Context(), jwt.MapClaims{"org": "acme", "team": "eng"})
	err = svc.DeleteSource(deleteCtx, "ds-covers")
	require.NoError(t, err)
}

func TestDeleteSource_CallerDoesNotCoverClaims(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	// Create source with claims (anonymous)
	createCtx := t.Context()
	_, err := svc.CreateSource(createCtx, "ds-no-cover", managedSourceReq(map[string]any{"org": "acme"}))
	require.NoError(t, err)

	// Delete with a caller whose JWT does NOT cover the source claims
	deleteCtx := auth.ContextWithClaims(t.Context(), jwt.MapClaims{"org": "contoso"})
	err = svc.DeleteSource(deleteCtx, "ds-no-cover")
	require.Error(t, err)
	assert.True(t, errors.Is(err, service.ErrClaimsInsufficient), "expected ErrClaimsInsufficient, got %v", err)
}

func TestListSources_FiltersByClaims(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	// Create two sources with different claims (anonymous)
	createCtx := t.Context()
	_, err := svc.CreateSource(createCtx, "ls-acme", managedSourceReq(map[string]any{"org": "acme"}))
	require.NoError(t, err)

	_, err = svc.CreateSource(createCtx, "ls-contoso", managedSourceReq(map[string]any{"org": "contoso"}))
	require.NoError(t, err)

	// List with acme JWT - should only see the acme source
	listCtx := auth.ContextWithClaims(t.Context(), jwt.MapClaims{"org": "acme"})
	sources, err := svc.ListSources(listCtx)
	require.NoError(t, err)

	names := sourceNames(sources)
	assert.Contains(t, names, "ls-acme")
	assert.NotContains(t, names, "ls-contoso")
}

func TestGetSourceByName_HiddenWhenClaimsDontMatch(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	// Create a source with claims (anonymous)
	createCtx := t.Context()
	_, err := svc.CreateSource(createCtx, "gs-hidden", managedSourceReq(map[string]any{"org": "acme"}))
	require.NoError(t, err)

	// Get with a non-matching JWT
	getCtx := auth.ContextWithClaims(t.Context(), jwt.MapClaims{"org": "contoso"})
	src, err := svc.GetSourceByName(getCtx, "gs-hidden")
	require.Error(t, err)
	assert.True(t, errors.Is(err, service.ErrSourceNotFound), "expected ErrSourceNotFound, got %v", err)
	assert.Nil(t, src)
}

func TestSourceCRUD_SuperAdminBypassesClaims(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	// Create source with acme claims (anonymous)
	createCtx := t.Context()
	_, err := svc.CreateSource(createCtx, "sa-source", managedSourceReq(map[string]any{"org": "acme"}))
	require.NoError(t, err)

	// Super-admin context with non-matching JWT claims should still work
	saCtx := auth.ContextWithClaims(t.Context(), jwt.MapClaims{"org": "other"})
	saCtx = auth.ContextWithRoles(saCtx, []auth.Role{auth.RoleSuperAdmin})

	// GetSourceByName
	src, err := svc.GetSourceByName(saCtx, "sa-source")
	require.NoError(t, err)
	require.NotNil(t, src)

	// UpdateSource - super-admin should cover non-matching claims
	updated, err := svc.UpdateSource(saCtx, "sa-source", managedSourceReq(map[string]any{"org": "neworg"}))
	require.NoError(t, err)
	require.NotNil(t, updated)

	// ListSources - super-admin sees all
	sources, err := svc.ListSources(saCtx)
	require.NoError(t, err)
	assert.Contains(t, sourceNames(sources), "sa-source")

	// DeleteSource
	err = svc.DeleteSource(saCtx, "sa-source")
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Registry CRUD — claim-scoped tests
// ---------------------------------------------------------------------------

// createSourceForRegistry is a helper that creates a managed source needed for registry tests.
func createSourceForRegistry(t *testing.T, svc *dbService, name string, claims map[string]any) {
	t.Helper()
	// Use anonymous context so claim check is skipped on creation
	ctx := t.Context()
	_, err := svc.CreateSource(ctx, name, managedSourceReq(claims))
	require.NoError(t, err)
}

func TestCreateRegistry_ClaimsWithinJWT(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	createSourceForRegistry(t, svc, "cr-within-src", map[string]any{"org": "acme"})

	ctx := auth.ContextWithClaims(t.Context(), jwt.MapClaims{"org": "acme", "team": "eng"})
	reg, err := svc.CreateRegistry(ctx, "cr-within-reg", &service.RegistryCreateRequest{
		Sources: []string{"cr-within-src"},
		Claims:  map[string]any{"org": "acme"},
	})
	require.NoError(t, err)
	require.NotNil(t, reg)
	assert.Equal(t, "cr-within-reg", reg.Name)
}

func TestCreateRegistry_ClaimsExceedJWT(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	createSourceForRegistry(t, svc, "cr-exceed-src", map[string]any{"org": "acme"})

	// JWT only has org=acme, but registry claims demand team=eng too
	ctx := auth.ContextWithClaims(t.Context(), jwt.MapClaims{"org": "acme"})
	reg, err := svc.CreateRegistry(ctx, "cr-exceed-reg", &service.RegistryCreateRequest{
		Sources: []string{"cr-exceed-src"},
		Claims:  map[string]any{"org": "acme", "team": "eng"},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, service.ErrClaimsInsufficient), "expected ErrClaimsInsufficient, got %v", err)
	assert.Nil(t, reg)
}

func TestCreateRegistry_SourceClaimsExceedCallerJWT(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	// Source has org=acme + team=eng claims
	createSourceForRegistry(t, svc, "cr-srcgate-src", map[string]any{"org": "acme", "team": "eng"})

	// Caller JWT only has org=acme (does not cover source's team=eng)
	ctx := auth.ContextWithClaims(t.Context(), jwt.MapClaims{"org": "acme"})
	reg, err := svc.CreateRegistry(ctx, "cr-srcgate-reg", &service.RegistryCreateRequest{
		Sources: []string{"cr-srcgate-src"},
		Claims:  map[string]any{"org": "acme"},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, service.ErrClaimsInsufficient), "expected ErrClaimsInsufficient, got %v", err)
	assert.Nil(t, reg)
}

func TestUpdateRegistry_CallerCoversExistingClaims(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	createSourceForRegistry(t, svc, "ur-covers-src", map[string]any{"org": "acme"})

	// Create registry (anonymous)
	createCtx := t.Context()
	_, err := svc.CreateRegistry(createCtx, "ur-covers-reg", &service.RegistryCreateRequest{
		Sources: []string{"ur-covers-src"},
		Claims:  map[string]any{"org": "acme"},
	})
	require.NoError(t, err)

	// Update with a caller whose JWT covers both old and new claims
	updateCtx := auth.ContextWithClaims(t.Context(), jwt.MapClaims{"org": "acme", "team": "eng"})
	updated, err := svc.UpdateRegistry(updateCtx, "ur-covers-reg", &service.RegistryCreateRequest{
		Sources: []string{"ur-covers-src"},
		Claims:  map[string]any{"org": "acme", "team": "eng"},
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
}

func TestUpdateRegistry_CallerDoesNotCoverExistingClaims(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	createSourceForRegistry(t, svc, "ur-nocover-src", map[string]any{"org": "acme"})

	// Create registry (anonymous)
	createCtx := t.Context()
	_, err := svc.CreateRegistry(createCtx, "ur-nocover-reg", &service.RegistryCreateRequest{
		Sources: []string{"ur-nocover-src"},
		Claims:  map[string]any{"org": "acme"},
	})
	require.NoError(t, err)

	// Update with a caller whose JWT does NOT cover existing claims
	updateCtx := auth.ContextWithClaims(t.Context(), jwt.MapClaims{"org": "contoso"})
	updated, err := svc.UpdateRegistry(updateCtx, "ur-nocover-reg", &service.RegistryCreateRequest{
		Sources: []string{"ur-nocover-src"},
		Claims:  map[string]any{"org": "contoso"},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, service.ErrClaimsInsufficient), "expected ErrClaimsInsufficient, got %v", err)
	assert.Nil(t, updated)
}

func TestDeleteRegistry_CallerCoversClaims(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	createSourceForRegistry(t, svc, "dr-covers-src", map[string]any{"org": "acme"})

	// Create registry (anonymous)
	createCtx := t.Context()
	_, err := svc.CreateRegistry(createCtx, "dr-covers-reg", &service.RegistryCreateRequest{
		Sources: []string{"dr-covers-src"},
		Claims:  map[string]any{"org": "acme"},
	})
	require.NoError(t, err)

	// Delete with a caller whose JWT covers the registry claims
	deleteCtx := auth.ContextWithClaims(t.Context(), jwt.MapClaims{"org": "acme"})
	err = svc.DeleteRegistry(deleteCtx, "dr-covers-reg")
	require.NoError(t, err)
}

func TestDeleteRegistry_CallerDoesNotCoverClaims(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	createSourceForRegistry(t, svc, "dr-nocover-src", map[string]any{"org": "acme"})

	// Create registry (anonymous)
	createCtx := t.Context()
	_, err := svc.CreateRegistry(createCtx, "dr-nocover-reg", &service.RegistryCreateRequest{
		Sources: []string{"dr-nocover-src"},
		Claims:  map[string]any{"org": "acme"},
	})
	require.NoError(t, err)

	// Delete with a caller whose JWT does NOT cover the registry claims
	deleteCtx := auth.ContextWithClaims(t.Context(), jwt.MapClaims{"org": "contoso"})
	err = svc.DeleteRegistry(deleteCtx, "dr-nocover-reg")
	require.Error(t, err)
	assert.True(t, errors.Is(err, service.ErrClaimsInsufficient), "expected ErrClaimsInsufficient, got %v", err)
}

func TestListRegistries_FiltersByClaims(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	createSourceForRegistry(t, svc, "lr-acme-src", map[string]any{"org": "acme"})
	createSourceForRegistry(t, svc, "lr-contoso-src", map[string]any{"org": "contoso"})

	// Create two registries with different claims (anonymous)
	createCtx := t.Context()
	_, err := svc.CreateRegistry(createCtx, "lr-acme-reg", &service.RegistryCreateRequest{
		Sources: []string{"lr-acme-src"},
		Claims:  map[string]any{"org": "acme"},
	})
	require.NoError(t, err)

	_, err = svc.CreateRegistry(createCtx, "lr-contoso-reg", &service.RegistryCreateRequest{
		Sources: []string{"lr-contoso-src"},
		Claims:  map[string]any{"org": "contoso"},
	})
	require.NoError(t, err)

	// List with acme JWT - should only see the acme registry
	listCtx := auth.ContextWithClaims(t.Context(), jwt.MapClaims{"org": "acme"})
	registries, err := svc.ListRegistries(listCtx)
	require.NoError(t, err)

	names := registryNames(registries)
	assert.Contains(t, names, "lr-acme-reg")
	assert.NotContains(t, names, "lr-contoso-reg")
}

func TestGetRegistryByName_HiddenWhenClaimsDontMatch(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	createSourceForRegistry(t, svc, "gr-hidden-src", map[string]any{"org": "acme"})

	// Create registry (anonymous)
	createCtx := t.Context()
	_, err := svc.CreateRegistry(createCtx, "gr-hidden-reg", &service.RegistryCreateRequest{
		Sources: []string{"gr-hidden-src"},
		Claims:  map[string]any{"org": "acme"},
	})
	require.NoError(t, err)

	// Get with non-matching JWT
	getCtx := auth.ContextWithClaims(t.Context(), jwt.MapClaims{"org": "contoso"})
	reg, err := svc.GetRegistryByName(getCtx, "gr-hidden-reg")
	require.Error(t, err)
	assert.True(t, errors.Is(err, service.ErrRegistryNotFound), "expected ErrRegistryNotFound, got %v", err)
	assert.Nil(t, reg)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// sourceNames extracts source names from a slice of SourceInfo.
func sourceNames(sources []service.SourceInfo) []string {
	names := make([]string, len(sources))
	for i, s := range sources {
		names[i] = s.Name
	}
	return names
}

// registryNames extracts registry names from a slice of RegistryInfo.
func registryNames(registries []service.RegistryInfo) []string {
	names := make([]string, len(registries))
	for i, r := range registries {
		names[i] = r.Name
	}
	return names
}
