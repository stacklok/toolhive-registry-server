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
	"github.com/stacklok/toolhive-registry-server/internal/sync/state"
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

// fileDataSourceReq returns a SourceCreateRequest for a file source with
// inline data and the given claims. Use this instead of managedSourceReq when
// a test needs multiple sources (since at most one managed source is allowed).
func fileDataSourceReq(claims map[string]any) *service.SourceCreateRequest {
	return &service.SourceCreateRequest{
		File: &config.FileConfig{
			Data: `{"version":"1.0.0","last_updated":"2025-01-15T10:30:00Z","servers":{}}`,
		},
		Claims: claims,
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

func TestCreateSource_ManagedLimitReached(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	ctx := t.Context()

	// First managed source succeeds
	_, err := svc.CreateSource(ctx, "first-managed", managedSourceReq(nil))
	require.NoError(t, err)

	// Second managed source is rejected
	_, err = svc.CreateSource(ctx, "second-managed", managedSourceReq(nil))
	require.Error(t, err)
	assert.True(t, errors.Is(err, service.ErrManagedSourceLimitReached), "expected ErrManagedSourceLimitReached, got %v", err)
}

func TestInitialize_RejectsConfigManagedWhenAPIManagedExists(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	ctx := t.Context()

	// Create an API-managed source
	_, err := svc.CreateSource(ctx, "api-managed", managedSourceReq(nil))
	require.NoError(t, err)

	// Attempt to initialize with a config that also has a managed source
	stateSvc := state.NewDBStateService(svc.pool)
	err = stateSvc.Initialize(ctx, &config.Config{
		Sources: []config.SourceConfig{
			{Name: "config-managed", Managed: &config.ManagedConfig{}},
		},
		Registries: []config.RegistryConfig{
			{Name: "reg", Sources: []string{"config-managed"}},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API-created managed source")
}

func TestCreateSource_NonManagedAllowedWhenManagedExists(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	ctx := t.Context()

	// Create a managed source
	_, err := svc.CreateSource(ctx, "the-managed", managedSourceReq(nil))
	require.NoError(t, err)

	// Creating a non-managed source should still succeed
	_, err = svc.CreateSource(ctx, "a-file-source", fileDataSourceReq(nil))
	require.NoError(t, err)
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

	// Create two sources with different claims (anonymous).
	// Use file-data sources so multiple can coexist (only one managed allowed).
	createCtx := t.Context()
	_, err := svc.CreateSource(createCtx, "ls-acme", fileDataSourceReq(map[string]any{"org": "acme"}))
	require.NoError(t, err)

	_, err = svc.CreateSource(createCtx, "ls-contoso", fileDataSourceReq(map[string]any{"org": "contoso"}))
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

// createSourceForRegistry is a helper that creates a file-data source needed for registry tests.
// Uses file-data (not managed) so multiple sources can coexist within a single test.
func createSourceForRegistry(t *testing.T, svc *dbService, name string, claims map[string]any) {
	t.Helper()
	// Use anonymous context so claim check is skipped on creation
	ctx := t.Context()
	_, err := svc.CreateSource(ctx, name, fileDataSourceReq(claims))
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
// Mixed-claims listing tests — verify streaming batch loops return all
// qualifying rows even when different claim values are interleaved.
// ---------------------------------------------------------------------------

func TestListSources_ReturnsAllMatchingWithMixedClaims(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	createCtx := t.Context()

	// Create 4 sources: 2 with org=acme, 1 with org=contoso, 1 with no claims.
	// Use file-data sources so multiple can coexist (only one managed allowed).
	_, err := svc.CreateSource(createCtx, "lsm-acme-1", fileDataSourceReq(map[string]any{"org": "acme"}))
	require.NoError(t, err)

	_, err = svc.CreateSource(createCtx, "lsm-acme-2", fileDataSourceReq(map[string]any{"org": "acme"}))
	require.NoError(t, err)

	_, err = svc.CreateSource(createCtx, "lsm-contoso", fileDataSourceReq(map[string]any{"org": "contoso"}))
	require.NoError(t, err)

	_, err = svc.CreateSource(createCtx, "lsm-open", fileDataSourceReq(nil))
	require.NoError(t, err)

	// List with acme JWT — should see both acme sources and the open (no-claims) source
	listCtx := auth.ContextWithClaims(t.Context(), jwt.MapClaims{"org": "acme"})
	sources, err := svc.ListSources(listCtx)
	require.NoError(t, err)

	names := sourceNames(sources)
	assert.Contains(t, names, "lsm-acme-1")
	assert.Contains(t, names, "lsm-acme-2")
	assert.NotContains(t, names, "lsm-contoso")
	assert.Contains(t, names, "lsm-open")
}

func TestListRegistries_ReturnsAllMatchingWithMixedClaims(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	createCtx := t.Context()

	// Create 4 sources (one per registry) with different claims
	createSourceForRegistry(t, svc, "lrm-acme-src-1", map[string]any{"org": "acme"})
	createSourceForRegistry(t, svc, "lrm-acme-src-2", map[string]any{"org": "acme"})
	createSourceForRegistry(t, svc, "lrm-contoso-src", map[string]any{"org": "contoso"})
	createSourceForRegistry(t, svc, "lrm-open-src", nil)

	// Create 4 registries with matching claims
	_, err := svc.CreateRegistry(createCtx, "lrm-acme-reg-1", &service.RegistryCreateRequest{
		Sources: []string{"lrm-acme-src-1"},
		Claims:  map[string]any{"org": "acme"},
	})
	require.NoError(t, err)

	_, err = svc.CreateRegistry(createCtx, "lrm-acme-reg-2", &service.RegistryCreateRequest{
		Sources: []string{"lrm-acme-src-2"},
		Claims:  map[string]any{"org": "acme"},
	})
	require.NoError(t, err)

	_, err = svc.CreateRegistry(createCtx, "lrm-contoso-reg", &service.RegistryCreateRequest{
		Sources: []string{"lrm-contoso-src"},
		Claims:  map[string]any{"org": "contoso"},
	})
	require.NoError(t, err)

	_, err = svc.CreateRegistry(createCtx, "lrm-open-reg", &service.RegistryCreateRequest{
		Sources: []string{"lrm-open-src"},
	})
	require.NoError(t, err)

	// List with acme JWT — should see both acme registries and the open one
	listCtx := auth.ContextWithClaims(t.Context(), jwt.MapClaims{"org": "acme"})
	registries, err := svc.ListRegistries(listCtx)
	require.NoError(t, err)

	names := registryNames(registries)
	assert.Contains(t, names, "lrm-acme-reg-1")
	assert.Contains(t, names, "lrm-acme-reg-2")
	assert.NotContains(t, names, "lrm-contoso-reg")
	assert.Contains(t, names, "lrm-open-reg")
}

func TestListSources_AnonymousReturnsAll(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	createCtx := t.Context()

	// Create 3 sources with different claims.
	// Use file-data sources so multiple can coexist (only one managed allowed).
	_, err := svc.CreateSource(createCtx, "lsa-acme", fileDataSourceReq(map[string]any{"org": "acme"}))
	require.NoError(t, err)

	_, err = svc.CreateSource(createCtx, "lsa-contoso", fileDataSourceReq(map[string]any{"org": "contoso"}))
	require.NoError(t, err)

	_, err = svc.CreateSource(createCtx, "lsa-open", fileDataSourceReq(nil))
	require.NoError(t, err)

	// List without JWT (anonymous) — should see all sources
	sources, err := svc.ListSources(t.Context())
	require.NoError(t, err)

	names := sourceNames(sources)
	assert.Contains(t, names, "lsa-acme")
	assert.Contains(t, names, "lsa-contoso")
	assert.Contains(t, names, "lsa-open")
}

func TestListRegistries_AnonymousReturnsAll(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)

	createCtx := t.Context()

	// Create 3 sources + 3 registries with different claims
	createSourceForRegistry(t, svc, "lra-acme-src", map[string]any{"org": "acme"})
	createSourceForRegistry(t, svc, "lra-contoso-src", map[string]any{"org": "contoso"})
	createSourceForRegistry(t, svc, "lra-open-src", nil)

	_, err := svc.CreateRegistry(createCtx, "lra-acme-reg", &service.RegistryCreateRequest{
		Sources: []string{"lra-acme-src"},
		Claims:  map[string]any{"org": "acme"},
	})
	require.NoError(t, err)

	_, err = svc.CreateRegistry(createCtx, "lra-contoso-reg", &service.RegistryCreateRequest{
		Sources: []string{"lra-contoso-src"},
		Claims:  map[string]any{"org": "contoso"},
	})
	require.NoError(t, err)

	_, err = svc.CreateRegistry(createCtx, "lra-open-reg", &service.RegistryCreateRequest{
		Sources: []string{"lra-open-src"},
	})
	require.NoError(t, err)

	// List without JWT (anonymous) — should see all registries
	registries, err := svc.ListRegistries(t.Context())
	require.NoError(t, err)

	names := registryNames(registries)
	assert.Contains(t, names, "lra-acme-reg")
	assert.Contains(t, names, "lra-contoso-reg")
	assert.Contains(t, names, "lra-open-reg")
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
