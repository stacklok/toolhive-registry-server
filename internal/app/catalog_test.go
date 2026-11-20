package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/database/dbtest"
	"github.com/stacklok/toolhive-registry-server/internal/app/storage"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/sync/standalone"
	standalonemocks "github.com/stacklok/toolhive-registry-server/internal/sync/standalone/mocks"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
	reconcilemocks "github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
)

// validInlineRegistry is a minimal upstream registry document that passes
// both formats.DecodeUpstream and persistence.ValidateSnapshot.
const validInlineRegistry = `{
  "$schema": "https://raw.githubusercontent.com/stacklok/toolhive-core/main/registry/types/data/upstream-registry.schema.json",
  "version": "1.0.0",
  "meta": {"last_updated": "2025-01-15T10:30:00Z"},
  "data": {"servers": [
    {"name": "io.example/weather", "description": "Weather server", "version": "1.0.0"}
  ]}
}`

// newTestDatabaseConfig returns a database config pointing at a fresh,
// migrated, isolated test database.
func newTestDatabaseConfig(t *testing.T) *config.DatabaseConfig {
	t.Helper()
	conn, cleanup := dbtest.SetupTestDB(t)
	t.Cleanup(cleanup)
	c := conn.Config()
	return &config.DatabaseConfig{
		Host:     c.Host,
		Port:     int(c.Port),
		User:     dbtest.DBUser,
		Password: dbtest.DBPass,
		Database: c.Database,
		SSLMode:  "disable",
	}
}

// newTestCatalog opens the library adapters over a fresh migrated database.
func newTestCatalog(t *testing.T) *storage.Catalog {
	t.Helper()
	ctx := context.Background()
	factory, err := storage.NewDatabaseFactory(ctx, &config.Config{Database: newTestDatabaseConfig(t)})
	require.NoError(t, err)
	t.Cleanup(factory.Cleanup)
	cat, err := factory.CreateCatalog(ctx, nil)
	require.NoError(t, err)
	return cat
}

func TestCatalogDefinitions_MapsSourceTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		watchNamespace string
		source         config.SourceConfig
		want           persistence.SourceDefinition
	}{
		{
			name: "git with auth and schedule",
			source: config.SourceConfig{
				Name: "git-src",
				Git: &config.GitConfig{
					Repository: "https://github.com/example/repo.git",
					Branch:     "main",
					Path:       "registry.json",
					Auth:       &config.GitAuthConfig{Username: "bot", PasswordFile: "/etc/secret/token"},
				},
				SyncPolicy: &config.SyncPolicyConfig{Interval: "30m"},
			},
			want: persistence.SourceDefinition{
				Name: "git-src",
				Git: &persistence.GitSpec{
					Repository: "https://github.com/example/repo.git",
					Branch:     "main",
					Path:       "registry.json",
					Auth:       &persistence.GitAuth{Username: "bot", PasswordFile: "/etc/secret/token"},
				},
				Schedule: "30m",
			},
		},
		{
			name: "git by tag without auth",
			source: config.SourceConfig{
				Name:       "git-tag",
				Git:        &config.GitConfig{Repository: "https://github.com/example/repo.git", Tag: "v1.0.0"},
				SyncPolicy: &config.SyncPolicyConfig{Interval: "1h"},
			},
			want: persistence.SourceDefinition{
				Name:     "git-tag",
				Git:      &persistence.GitSpec{Repository: "https://github.com/example/repo.git", Tag: "v1.0.0"},
				Schedule: "1h",
			},
		},
		{
			name: "api with timeout",
			source: config.SourceConfig{
				Name:       "api-src",
				API:        &config.APIConfig{Endpoint: "https://registry.example.com", Timeout: "30s"},
				SyncPolicy: &config.SyncPolicyConfig{Interval: "15m"},
			},
			want: persistence.SourceDefinition{
				Name:     "api-src",
				API:      &persistence.APISpec{Endpoint: "https://registry.example.com", Timeout: "30s"},
				Schedule: "15m",
			},
		},
		{
			name: "file path",
			source: config.SourceConfig{
				Name:       "file-path",
				File:       &config.FileConfig{Path: "/data/registry.json"},
				SyncPolicy: &config.SyncPolicyConfig{Interval: "5m"},
			},
			want: persistence.SourceDefinition{
				Name:     "file-path",
				File:     &persistence.FileSpec{Path: "/data/registry.json"},
				Schedule: "5m",
			},
		},
		{
			name: "file url with timeout",
			source: config.SourceConfig{
				Name:       "file-url",
				File:       &config.FileConfig{URL: "https://example.com/registry.json", Timeout: "10s"},
				SyncPolicy: &config.SyncPolicyConfig{Interval: "5m"},
			},
			want: persistence.SourceDefinition{
				Name:     "file-url",
				File:     &persistence.FileSpec{URL: "https://example.com/registry.json", Timeout: "10s"},
				Schedule: "5m",
			},
		},
		{
			name:   "file inline data has no schedule",
			source: config.SourceConfig{Name: "file-data", File: &config.FileConfig{Data: validInlineRegistry}},
			want: persistence.SourceDefinition{
				Name: "file-data",
				File: &persistence.FileSpec{Data: validInlineRegistry},
			},
		},
		{
			name:   "managed",
			source: config.SourceConfig{Name: "managed-src", Managed: &config.ManagedConfig{}},
			want:   persistence.SourceDefinition{Name: "managed-src", Managed: &persistence.ManagedSpec{}},
		},
		{
			name: "kubernetes explicit namespaces are trimmed sorted and deduplicated",
			source: config.SourceConfig{
				Name:       "k8s-src",
				Kubernetes: &config.KubernetesConfig{Namespaces: []string{"prod", " dev ", "prod"}},
			},
			watchNamespace: "ignored",
			want: persistence.SourceDefinition{
				Name:       "k8s-src",
				Kubernetes: &persistence.KubernetesSpec{Namespaces: []string{"dev", "prod"}},
			},
		},
		{
			name:           "kubernetes falls back to watch namespace",
			source:         config.SourceConfig{Name: "k8s-watch", Kubernetes: &config.KubernetesConfig{}},
			watchNamespace: "team-b,team-a",
			want: persistence.SourceDefinition{
				Name:       "k8s-watch",
				Kubernetes: &persistence.KubernetesSpec{Namespaces: []string{"team-a", "team-b"}},
			},
		},
		{
			name:   "kubernetes without any namespace scope watches all",
			source: config.SourceConfig{Name: "k8s-all", Kubernetes: &config.KubernetesConfig{}},
			want: persistence.SourceDefinition{
				Name:       "k8s-all",
				Kubernetes: &persistence.KubernetesSpec{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &config.Config{WatchNamespace: tt.watchNamespace, Sources: []config.SourceConfig{tt.source}}
			sources, views, err := catalogDefinitions(cfg)
			require.NoError(t, err)
			require.Len(t, sources, 1)
			assert.Empty(t, views)
			assert.Equal(t, tt.want, sources[0])
		})
	}
}

func TestCatalogDefinitions_MapsRegistries(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Sources: []config.SourceConfig{
			{Name: "managed-src", Managed: &config.ManagedConfig{}},
			{Name: "inline", File: &config.FileConfig{Data: validInlineRegistry}},
		},
		Registries: []config.RegistryConfig{
			{Name: "all", Sources: []string{"inline", "managed-src"}, Claims: map[string]any{"org": "acme"}},
			{Name: "managed-only", Sources: []string{"managed-src"}},
		},
	}

	sources, views, err := catalogDefinitions(cfg)
	require.NoError(t, err)
	assert.Len(t, sources, 2)
	assert.Equal(t, []persistence.ViewDefinition{
		{Name: "all", Sources: []string{"inline", "managed-src"}},
		{Name: "managed-only", Sources: []string{"managed-src"}},
	}, views, "registry order and source order are preserved; claims never reach the library")
}

func TestCatalogDefinitions_IgnoresSyncPolicyOnNonPolledSources(t *testing.T) {
	t.Parallel()

	policy := &config.SyncPolicyConfig{Interval: "10m"}
	tests := []struct {
		name   string
		source config.SourceConfig
	}{
		{name: "managed", source: config.SourceConfig{Name: "m", Managed: &config.ManagedConfig{}, SyncPolicy: policy}},
		{
			name:   "kubernetes",
			source: config.SourceConfig{Name: "k", Kubernetes: &config.KubernetesConfig{}, SyncPolicy: policy},
		},
		{
			name:   "inline file data",
			source: config.SourceConfig{Name: "d", File: &config.FileConfig{Data: validInlineRegistry}, SyncPolicy: policy},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sources, _, err := catalogDefinitions(&config.Config{Sources: []config.SourceConfig{tt.source}})
			require.NoError(t, err, "a syncPolicy on a non-polled source is a warning, not an error")
			require.Len(t, sources, 1)
			assert.Empty(t, sources[0].Schedule)
		})
	}
}

func TestCatalogDefinitions_Filter(t *testing.T) {
	t.Parallel()

	filter := &config.FilterConfig{
		Names: &config.NameFilterConfig{Include: []string{"io.example/*"}, Exclude: []string{"*-beta"}},
		Tags:  &config.TagFilterConfig{Include: []string{"stable"}, Exclude: []string{"deprecated"}},
	}
	wantFilter := &persistence.Filter{
		Names: &persistence.NameFilter{Include: []string{"io.example/*"}, Exclude: []string{"*-beta"}},
		Tags:  &persistence.NameFilter{Include: []string{"stable"}, Exclude: []string{"deprecated"}},
	}

	tests := []struct {
		name       string
		source     config.SourceConfig
		wantFilter *persistence.Filter
	}{
		{
			name: "kept on polled git source",
			source: config.SourceConfig{
				Name: "git-src", Git: &config.GitConfig{Repository: "https://github.com/example/repo.git"},
				SyncPolicy: &config.SyncPolicyConfig{Interval: "1h"}, Filter: filter,
			},
			wantFilter: wantFilter,
		},
		{
			name: "kept on polled file source",
			source: config.SourceConfig{
				Name: "file-src", File: &config.FileConfig{Path: "/data/registry.json"},
				SyncPolicy: &config.SyncPolicyConfig{Interval: "1h"}, Filter: filter,
			},
			wantFilter: wantFilter,
		},
		{
			name: "names only",
			source: config.SourceConfig{
				Name: "api-src", API: &config.APIConfig{Endpoint: "https://registry.example.com"},
				SyncPolicy: &config.SyncPolicyConfig{Interval: "1h"},
				Filter:     &config.FilterConfig{Names: &config.NameFilterConfig{Include: []string{"a"}}},
			},
			wantFilter: &persistence.Filter{Names: &persistence.NameFilter{Include: []string{"a"}}},
		},
		{
			name:   "ignored on managed source",
			source: config.SourceConfig{Name: "m", Managed: &config.ManagedConfig{}, Filter: filter},
		},
		{
			name:   "ignored on kubernetes source",
			source: config.SourceConfig{Name: "k", Kubernetes: &config.KubernetesConfig{}, Filter: filter},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sources, _, err := catalogDefinitions(&config.Config{Sources: []config.SourceConfig{tt.source}})
			require.NoError(t, err)
			require.Len(t, sources, 1)
			assert.Equal(t, tt.wantFilter, sources[0].Filter)
		})
	}
}

// TestCatalogDefinitions_FilterOnInlineDataSource checks that a v1.5.4
// config with `file: {data: ...}` plus a `filter`, which v1.5.4 accepted and
// applied, still starts and keeps the filter.
func TestCatalogDefinitions_FilterOnInlineDataSource(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{Sources: []config.SourceConfig{{
		Name:   "inline",
		File:   &config.FileConfig{Data: validInlineRegistry},
		Filter: &config.FilterConfig{Names: &config.NameFilterConfig{Include: []string{"io.example/*"}}},
	}}}
	sources, _, err := catalogDefinitions(cfg)
	require.NoError(t, err)
	require.Len(t, sources, 1)
	require.NotNil(t, sources[0].Filter)
}

func TestCatalogDefinitions_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cfg         *config.Config
		errContains string
		wantInvalid bool
	}{
		{
			name:        "source without type",
			cfg:         &config.Config{Sources: []config.SourceConfig{{Name: "nothing"}}},
			errContains: `source "nothing" has no type`,
			wantInvalid: true,
		},
		{
			name: "inline data that is not JSON",
			cfg: &config.Config{Sources: []config.SourceConfig{
				{Name: "bad", File: &config.FileConfig{Data: "not json"}},
			}},
			errContains: `source "bad" has invalid inline registry data`,
			wantInvalid: true,
		},
		{
			name: "inline data without servers",
			cfg: &config.Config{Sources: []config.SourceConfig{{Name: "empty", File: &config.FileConfig{
				Data: `{"version":"1.0.0","meta":{"last_updated":"2025-01-15T10:30:00Z"},"data":{"servers":[]}}`,
			}}}},
			errContains: `source "empty" has invalid inline registry data`,
			wantInvalid: true,
		},
		{
			name: "inline data that decodes but fails snapshot validation",
			cfg: &config.Config{Sources: []config.SourceConfig{{Name: "noversion", File: &config.FileConfig{
				Data: `{"version":"1.0.0","meta":{"last_updated":"2025-01-15T10:30:00Z"},` +
					`"data":{"servers":[{"name":"io.example/weather","description":"Weather"}]}}`,
			}}}},
			errContains: "",
			wantInvalid: true,
		},
		{
			name: "polled source without schedule",
			cfg: &config.Config{Sources: []config.SourceConfig{{
				Name: "api-src", API: &config.APIConfig{Endpoint: "https://registry.example.com"},
			}}},
			errContains: `source "api-src"`,
			wantInvalid: true,
		},
		{
			name:        "invalid source name",
			cfg:         &config.Config{Sources: []config.SourceConfig{{Name: "Bad_Name", Managed: &config.ManagedConfig{}}}},
			errContains: `source "Bad_Name"`,
			wantInvalid: true,
		},
		{
			name: "kubernetes namespace scope with an empty entry",
			cfg: &config.Config{Sources: []config.SourceConfig{
				{Name: "k8s", Kubernetes: &config.KubernetesConfig{Namespaces: []string{"ok", " "}}},
			}},
			errContains: "empty namespace",
			wantInvalid: true,
		},
		{
			name: "kubernetes watch namespace with an invalid name",
			cfg: &config.Config{
				WatchNamespace: "Not_Valid",
				Sources:        []config.SourceConfig{{Name: "k8s", Kubernetes: &config.KubernetesConfig{}}},
			},
			errContains: "invalid namespace scope",
			wantInvalid: true,
		},
		{
			name: "registry references unknown source",
			cfg: &config.Config{
				Sources:    []config.SourceConfig{{Name: "managed-src", Managed: &config.ManagedConfig{}}},
				Registries: []config.RegistryConfig{{Name: "main", Sources: []string{"missing"}}},
			},
			errContains: "outside the config set",
			wantInvalid: true,
		},
		{
			name: "duplicate source names",
			cfg: &config.Config{Sources: []config.SourceConfig{
				{Name: "dup", Managed: &config.ManagedConfig{}},
				{Name: "dup", File: &config.FileConfig{Data: validInlineRegistry}},
			}},
			errContains: "duplicate source",
			wantInvalid: true,
		},
		{
			name: "two managed sources",
			cfg: &config.Config{Sources: []config.SourceConfig{
				{Name: "m1", Managed: &config.ManagedConfig{}},
				{Name: "m2", Managed: &config.ManagedConfig{}},
			}},
			errContains: "only one managed source",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := catalogDefinitions(tt.cfg)
			require.Error(t, err)
			if tt.errContains != "" {
				assert.Contains(t, err.Error(), tt.errContains)
			}
			if tt.wantInvalid {
				assert.ErrorIs(t, err, persistence.ErrInvalid)
			}
		})
	}
}

func TestSourcePreflight(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		def     persistence.SourceDefinition
		wantErr bool
	}{
		{name: "managed", def: persistence.SourceDefinition{Name: "m", Managed: &persistence.ManagedSpec{}}},
		{
			name: "valid inline data",
			def:  persistence.SourceDefinition{Name: "d", File: &persistence.FileSpec{Data: validInlineRegistry}},
		},
		{
			name: "valid glob patterns",
			def: persistence.SourceDefinition{
				Name: "g", Git: &persistence.GitSpec{Repository: "https://github.com/example/repo.git"}, Schedule: "1h",
				Filter: &persistence.Filter{Names: &persistence.NameFilter{Include: []string{"io.*/*", "x?"}, Exclude: []string{"*-rc"}}},
			},
		},
		{
			name: "tag patterns are not glob-checked",
			def: persistence.SourceDefinition{
				Name: "g", Git: &persistence.GitSpec{Repository: "https://github.com/example/repo.git"}, Schedule: "1h",
				Filter: &persistence.Filter{Tags: &persistence.NameFilter{Include: []string{"[unclosed"}}},
			},
		},
		{name: "no spec", def: persistence.SourceDefinition{Name: "none"}, wantErr: true},
		{
			name: "two specs",
			def: persistence.SourceDefinition{
				Name: "two", Managed: &persistence.ManagedSpec{}, File: &persistence.FileSpec{Data: validInlineRegistry},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := sourcePreflight(tt.def)
			if tt.wantErr {
				require.ErrorIs(t, err, persistence.ErrInvalid)
				return
			}
			require.NoError(t, err)
		})
	}
}

// claimsFixture holds the IDs of the rows TestApplyConfigClaims inspects.
type claimsFixture struct {
	pool    *pgxpool.Pool
	sources map[string]uuid.UUID
	entries map[string]uuid.UUID
}

func claimsTestConfig() *config.Config {
	return &config.Config{
		Sources: []config.SourceConfig{
			{
				Name: "git-src", Git: &config.GitConfig{Repository: "https://github.com/example/repo.git"},
				SyncPolicy: &config.SyncPolicyConfig{Interval: "1h"}, Claims: map[string]any{"org": "acme"},
			},
			{
				Name: "api-src", API: &config.APIConfig{Endpoint: "https://registry.example.com"},
				SyncPolicy: &config.SyncPolicyConfig{Interval: "1h"},
			},
			{
				Name: "file-src", File: &config.FileConfig{Path: "/data/registry.json"},
				SyncPolicy: &config.SyncPolicyConfig{Interval: "1h"}, Claims: map[string]any{"team": []any{"a", "b"}},
			},
			{Name: "managed-src", Managed: &config.ManagedConfig{}, Claims: map[string]any{"org": "acme"}},
			{
				Name: "k8s-src", Kubernetes: &config.KubernetesConfig{Namespaces: []string{"default"}},
				Claims: map[string]any{"org": "acme"},
			},
		},
		Registries: []config.RegistryConfig{
			{
				Name:    "main",
				Sources: []string{"git-src", "api-src", "file-src", "managed-src", "k8s-src"},
				Claims:  map[string]any{"org": []any{"acme", "community"}},
			},
			{Name: "open", Sources: []string{"managed-src"}},
		},
	}
}

// newClaimsFixture reconciles claimsTestConfig into a fresh database and
// seeds one entry per source with pre-existing claims, plus registry_sync
// rows carrying an applied baseline for the git and kubernetes sources.
func newClaimsFixture(t *testing.T, cfg *config.Config) claimsFixture {
	t.Helper()
	ctx := context.Background()
	cat := newTestCatalog(t)

	sources, views, err := catalogDefinitions(cfg)
	require.NoError(t, err)
	require.NoError(t, cat.Definitions.Reconcile(ctx, sources, views))

	f := claimsFixture{pool: cat.Pool, sources: map[string]uuid.UUID{}, entries: map[string]uuid.UUID{}}
	seed := map[string]*string{
		"git-src":     ptr(`{"org":"stale"}`),
		"api-src":     ptr(`{"org":"leftover"}`),
		"file-src":    nil,
		"managed-src": ptr(`{"org":"published"}`),
		"k8s-src":     ptr(`{"org":"annotation"}`),
	}
	for name, claims := range seed {
		var sourceID uuid.UUID
		require.NoError(t, cat.Pool.QueryRow(ctx, `SELECT id FROM source WHERE name = $1`, name).Scan(&sourceID))
		f.sources[name] = sourceID

		var entryID uuid.UUID
		require.NoError(t, cat.Pool.QueryRow(ctx, `
INSERT INTO registry_entry (source_id, entry_type, name, claims)
VALUES ($1, 'MCP', $2, $3::jsonb)
RETURNING id`, sourceID, "io.example/"+name, claims).Scan(&entryID))
		f.entries[name] = entryID
	}
	for _, name := range []string{"git-src", "k8s-src"} {
		_, err := cat.Pool.Exec(ctx, `
INSERT INTO registry_sync (source_id, sync_status, applied_generation)
VALUES ($1, 'COMPLETED', 5)`, f.sources[name])
		require.NoError(t, err)
	}
	return f
}

func ptr[T any](v T) *T { return &v }

func (f claimsFixture) jsonColumn(t *testing.T, query string, args ...any) *string {
	t.Helper()
	var out *string
	require.NoError(t, f.pool.QueryRow(context.Background(), query, args...).Scan(&out))
	return out
}

func (f claimsFixture) sourceClaims(t *testing.T, name string) *string {
	t.Helper()
	return f.jsonColumn(t, `SELECT claims::text FROM source WHERE name = $1`, name)
}

func (f claimsFixture) registryClaims(t *testing.T, name string) *string {
	t.Helper()
	return f.jsonColumn(t, `SELECT claims::text FROM registry WHERE name = $1`, name)
}

func (f claimsFixture) entryClaims(t *testing.T, source string) *string {
	t.Helper()
	return f.jsonColumn(t, `SELECT claims::text FROM registry_entry WHERE id = $1`, f.entries[source])
}

func (f claimsFixture) appliedGeneration(t *testing.T, source string) *int64 {
	t.Helper()
	var out *int64
	require.NoError(t, f.pool.QueryRow(context.Background(),
		`SELECT applied_generation FROM registry_sync WHERE source_id = $1`, f.sources[source]).Scan(&out))
	return out
}

func assertJSONColumn(t *testing.T, want string, got *string, msgAndArgs ...any) {
	t.Helper()
	if want == "" {
		assert.Nil(t, got, msgAndArgs...)
		return
	}
	require.NotNil(t, got, msgAndArgs...)
	assert.JSONEq(t, want, *got, msgAndArgs...)
}

func TestApplyConfigClaims_StoresAndPropagatesClaims(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := claimsTestConfig()
	f := newClaimsFixture(t, cfg)

	require.NoError(t, applyConfigClaims(ctx, f.pool, cfg))

	// Config claims land on CONFIG source and registry rows; empty → NULL.
	assertJSONColumn(t, `{"org":"acme"}`, f.sourceClaims(t, "git-src"))
	assertJSONColumn(t, "", f.sourceClaims(t, "api-src"), "no claims are stored as SQL NULL")
	assertJSONColumn(t, `{"team":["a","b"]}`, f.sourceClaims(t, "file-src"))
	assertJSONColumn(t, `{"org":"acme"}`, f.sourceClaims(t, "managed-src"))
	assertJSONColumn(t, `{"org":"acme"}`, f.sourceClaims(t, "k8s-src"))
	assertJSONColumn(t, `{"org":["acme","community"]}`, f.registryClaims(t, "main"))
	assertJSONColumn(t, "", f.registryClaims(t, "open"))

	// git/api/file entries take their source's claims.
	assertJSONColumn(t, `{"org":"acme"}`, f.entryClaims(t, "git-src"))
	assertJSONColumn(t, "", f.entryClaims(t, "api-src"), "a source without claims clears entry claims")
	assertJSONColumn(t, `{"team":["a","b"]}`, f.entryClaims(t, "file-src"))

	// Managed entries keep their own published claims (v1.5.4 reset them).
	assertJSONColumn(t, `{"org":"published"}`, f.entryClaims(t, "managed-src"),
		"per-entry published claims must survive a restart")
	// Kubernetes entries keep annotation claims; a resync recomputes them.
	assertJSONColumn(t, `{"org":"annotation"}`, f.entryClaims(t, "k8s-src"))

	// Changed kubernetes claims invalidate its sync baseline only.
	assert.Nil(t, f.appliedGeneration(t, "k8s-src"), "changed kubernetes claims force a full resync")
	assert.Equal(t, ptr(int64(5)), f.appliedGeneration(t, "git-src"), "other baselines are untouched")
}

func TestApplyConfigClaims_RestartIsIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := claimsTestConfig()
	f := newClaimsFixture(t, cfg)
	require.NoError(t, applyConfigClaims(ctx, f.pool, cfg))

	// Simulate a completed resync and a managed claim update after startup.
	_, err := f.pool.Exec(ctx, `UPDATE registry_sync SET applied_generation = 7 WHERE source_id = $1`, f.sources["k8s-src"])
	require.NoError(t, err)
	_, err = f.pool.Exec(ctx, `UPDATE registry_entry SET claims = '{"org":"updated"}' WHERE id = $1`, f.entries["managed-src"])
	require.NoError(t, err)

	// A restart with the same config is a no-op for both.
	require.NoError(t, applyConfigClaims(ctx, f.pool, cfg))
	assert.Equal(t, ptr(int64(7)), f.appliedGeneration(t, "k8s-src"), "unchanged kubernetes claims keep the baseline")
	assertJSONColumn(t, `{"org":"updated"}`, f.entryClaims(t, "managed-src"))
	assertJSONColumn(t, `{"org":"acme"}`, f.entryClaims(t, "git-src"))
}

func TestApplyConfigClaims_ClaimChangesOnRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := claimsTestConfig()
	f := newClaimsFixture(t, cfg)
	require.NoError(t, applyConfigClaims(ctx, f.pool, cfg))
	_, err := f.pool.Exec(ctx, `UPDATE registry_sync SET applied_generation = 7 WHERE source_id = $1`, f.sources["k8s-src"])
	require.NoError(t, err)

	// Operator edits claims: git → new org, managed → removed, k8s → removed,
	// registry → removed.
	cfg.Sources[0].Claims = map[string]any{"org": "beta"}
	cfg.Sources[3].Claims = nil
	cfg.Sources[4].Claims = map[string]any{}
	cfg.Registries[0].Claims = nil

	require.NoError(t, applyConfigClaims(ctx, f.pool, cfg))
	assertJSONColumn(t, `{"org":"beta"}`, f.sourceClaims(t, "git-src"))
	assertJSONColumn(t, `{"org":"beta"}`, f.entryClaims(t, "git-src"))
	assertJSONColumn(t, "", f.sourceClaims(t, "managed-src"))
	assertJSONColumn(t, `{"org":"published"}`, f.entryClaims(t, "managed-src"),
		"removing managed source claims must not touch entry claims")
	assertJSONColumn(t, "", f.sourceClaims(t, "k8s-src"), "an empty claims map is stored as NULL")
	assert.Nil(t, f.appliedGeneration(t, "k8s-src"), "removed kubernetes claims force a full resync")
	assertJSONColumn(t, "", f.registryClaims(t, "main"))
}

func TestApplyConfigClaims_RecordsManagedSyncStatus(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := claimsTestConfig()
	f := newClaimsFixture(t, cfg)

	syncRow := func(source string) (status string, msg *string, ended *time.Time) {
		t.Helper()
		require.NoError(t, f.pool.QueryRow(ctx,
			`SELECT sync_status::text, error_msg, ended_at FROM registry_sync WHERE source_id = $1`,
			f.sources[source]).Scan(&status, &msg, &ended))
		return status, msg, ended
	}
	rowCount := func(source string) int {
		t.Helper()
		var n int
		require.NoError(t, f.pool.QueryRow(ctx,
			`SELECT count(*) FROM registry_sync WHERE source_id = $1`, f.sources[source]).Scan(&n))
		return n
	}

	require.NoError(t, applyConfigClaims(ctx, f.pool, cfg))
	status, msg, ended := syncRow("managed-src")
	assert.Equal(t, "COMPLETED", status, "a fresh managed source reports a completed status, as in v1.5.4")
	require.NotNil(t, msg)
	assert.Equal(t, "Non-synced source (type: managed)", *msg)
	assert.Nil(t, ended, "no sync ever ran, so there is no last sync time")
	for _, name := range []string{"api-src", "file-src"} {
		assert.Zero(t, rowCount(name), "synced sources get their status from their first sync job")
	}

	// A restart leaves an existing row alone.
	_, err := f.pool.Exec(ctx, `UPDATE registry_sync SET error_msg = 'kept' WHERE source_id = $1`, f.sources["managed-src"])
	require.NoError(t, err)
	require.NoError(t, applyConfigClaims(ctx, f.pool, cfg))
	_, msg, _ = syncRow("managed-src")
	require.NotNil(t, msg)
	assert.Equal(t, "kept", *msg)
}

func TestApplyConfigClaims_DoesNotTouchAPIRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat := newTestCatalog(t)

	// A config registry entry naming an API-owned registry must not
	// overwrite its claims: SetConfigRegistryClaims only updates CONFIG rows.
	cfg := &config.Config{
		Sources:    []config.SourceConfig{{Name: "managed-src", Managed: &config.ManagedConfig{}}},
		Registries: []config.RegistryConfig{{Name: "main", Sources: []string{"managed-src"}}},
	}
	sources, views, err := catalogDefinitions(cfg)
	require.NoError(t, err)
	require.NoError(t, cat.Definitions.Reconcile(ctx, sources, views))
	_, err = cat.Definitions.CreateView(ctx, persistence.ViewDefinition{Name: "api-view", Sources: []string{"managed-src"}})
	require.NoError(t, err)
	_, err = cat.Pool.Exec(ctx, `UPDATE registry SET claims = '{"org":"api"}' WHERE name = 'api-view'`)
	require.NoError(t, err)

	cfg.Registries = append(cfg.Registries, config.RegistryConfig{
		Name: "api-view", Sources: []string{"managed-src"}, Claims: map[string]any{"org": "config"},
	})
	require.NoError(t, applyConfigClaims(ctx, cat.Pool, cfg))

	var claims string
	require.NoError(t, cat.Pool.QueryRow(ctx, `SELECT claims::text FROM registry WHERE name = 'api-view'`).Scan(&claims))
	assert.JSONEq(t, `{"org":"api"}`, claims, "claims of API-created registries are never overwritten from config")
}

func TestApplyConfigClaims_Errors(t *testing.T) {
	t.Parallel()
	cat := newTestCatalog(t)

	t.Run("missing synced source row", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{Sources: []config.SourceConfig{{
			Name: "ghost", Git: &config.GitConfig{Repository: "https://github.com/example/repo.git"},
			SyncPolicy: &config.SyncPolicyConfig{Interval: "1h"},
		}}}
		err := applyConfigClaims(context.Background(), cat.Pool, cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read source ghost")
	})

	t.Run("cancelled context", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := applyConfigClaims(ctx, cat.Pool, &config.Config{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to store config claims")
	})

	t.Run("missing managed and kubernetes rows are skipped", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Sources: []config.SourceConfig{
				{Name: "no-managed", Managed: &config.ManagedConfig{}, Claims: map[string]any{"a": "b"}},
				{Name: "no-k8s", Kubernetes: &config.KubernetesConfig{}, Claims: map[string]any{"a": "b"}},
			},
			Registries: []config.RegistryConfig{{Name: "no-registry", Claims: map[string]any{"a": "b"}}},
		}
		require.NoError(t, applyConfigClaims(context.Background(), cat.Pool, cfg),
			"updates that match no CONFIG row are not errors")
	})
}

// newTestRuntime builds a standalone runtime with no sources whose poller
// never claims anything, so Run idles until cancelled.
func newTestRuntime(t *testing.T, listErr error) *standalone.Runtime {
	t.Helper()
	ctrl := gomock.NewController(t)
	sources := standalonemocks.NewMockSources(ctrl)
	sources.EXPECT().ListSources(gomock.Any()).Return(nil, listErr).AnyTimes()
	jobs := reconcilemocks.NewMockJobs(ctrl)
	jobs.EXPECT().ClaimNext(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ time.Duration) (persistence.Job, bool, error) {
			select {
			case <-ctx.Done():
				return persistence.Job{}, false, ctx.Err()
			case <-time.After(5 * time.Millisecond):
				return persistence.Job{}, false, nil
			}
		}).AnyTimes()
	fetcher := reconcilemocks.NewMockFetcher(ctrl)

	runtime, err := standalone.New(sources, jobs, fetcher, standalone.Options{
		Reconcile: reconcile.Options{PollInterval: time.Hour, LeaseDuration: time.Minute},
		Operator: func(persistence.SourceDefinition) (standalone.Worker, error) {
			return nil, errors.New("no operator in this test")
		},
		Preflight: func(context.Context, persistence.SourceDefinition) error { return nil },
	})
	require.NoError(t, err)
	return runtime
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	require.Eventually(t, cond, 5*time.Second, 5*time.Millisecond, msg)
}

func TestSyncRunner_StopBeforeStartIsNoop(t *testing.T) {
	t.Parallel()
	runner := &syncRunner{runtime: newTestRuntime(t, nil)}
	require.NoError(t, runner.Stop())
	require.NoError(t, runner.Stop())
	assert.False(t, runner.Ready())

	// A Start that races behind Stop must not begin a runtime nothing will stop.
	done := make(chan error, 1)
	go func() { done <- runner.Start(context.Background()) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Start after Stop did not return immediately")
	}
	assert.False(t, runner.Ready())
}

func TestSyncRunner_StopCancelsRunningStart(t *testing.T) {
	t.Parallel()
	runner := &syncRunner{runtime: newTestRuntime(t, nil)}

	done := make(chan error, 1)
	go func() { done <- runner.Start(context.Background()) }()
	waitFor(t, runner.Ready, "runtime did not become ready")

	require.NoError(t, runner.Stop(), "Stop returns nil on cancellation")
	select {
	case err := <-done:
		require.NoError(t, err, "Start returns nil when stopped")
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after Stop")
	}
	assert.False(t, runner.Ready())
	require.NoError(t, runner.Stop(), "a second Stop after shutdown is a no-op")
}

func TestSyncRunner_ParentContextCancellation(t *testing.T) {
	t.Parallel()
	runner := &syncRunner{runtime: newTestRuntime(t, nil)}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Start(ctx) }()
	waitFor(t, runner.Ready, "runtime did not become ready")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err, "Start returns nil when its context ends")
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after context cancellation")
	}
	require.NoError(t, runner.Stop())
}

func TestSyncRunner_RestartsFailedRuntime(t *testing.T) {
	t.Parallel()
	listErr := errors.New("list failed")
	ctrl := gomock.NewController(t)
	sources := standalonemocks.NewMockSources(ctrl)
	// The database fails twice, then recovers.
	var lists atomic.Int32
	sources.EXPECT().ListSources(gomock.Any()).DoAndReturn(func(context.Context) ([]persistence.SourceDefinition, error) {
		if lists.Add(1) <= 2 {
			return nil, listErr
		}
		return nil, nil
	}).AnyTimes()
	jobs := reconcilemocks.NewMockJobs(ctrl)
	jobs.EXPECT().ClaimNext(gomock.Any(), gomock.Any()).Return(persistence.Job{}, false, nil).AnyTimes()
	runtime, err := standalone.New(sources, jobs, reconcilemocks.NewMockFetcher(ctrl), standalone.Options{
		Reconcile: reconcile.Options{PollInterval: time.Hour, LeaseDuration: time.Minute},
		Operator: func(persistence.SourceDefinition) (standalone.Worker, error) {
			return nil, errors.New("no operator in this test")
		},
		Preflight: func(context.Context, persistence.SourceDefinition) error { return nil },
	})
	require.NoError(t, err)
	runner := &syncRunner{runtime: runtime, restartMin: time.Millisecond, restartMax: 4 * time.Millisecond}

	done := make(chan error, 1)
	go func() { done <- runner.Start(context.Background()) }()
	waitFor(t, runner.Ready, "runtime was not restarted after failures")
	require.GreaterOrEqual(t, lists.Load(), int32(3))

	require.NoError(t, runner.Stop())
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after Stop")
	}
}

func TestSyncRunner_StopDuringRestartDelay(t *testing.T) {
	t.Parallel()
	runner := &syncRunner{runtime: newTestRuntime(t, errors.New("list failed")), restartMin: time.Hour}

	done := make(chan error, 1)
	go func() { done <- runner.Start(context.Background()) }()
	time.Sleep(20 * time.Millisecond) // let the first run fail and the delay begin
	require.NoError(t, runner.Stop())
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not end the restart delay")
	}
	assert.False(t, runner.Ready())
}

func TestSyncRunner_StopRunsConcurrentlyWithStart(t *testing.T) {
	t.Parallel()
	runner := &syncRunner{runtime: newTestRuntime(t, nil)}

	var started atomic.Bool
	done := make(chan error, 1)
	go func() {
		started.Store(true)
		done <- runner.Start(context.Background())
	}()
	waitFor(t, runner.Ready, "runtime did not become ready")
	require.True(t, started.Load())

	stopped := make(chan error, 2)
	for range 2 {
		go func() { stopped <- runner.Stop() }()
	}
	for range 2 {
		select {
		case err := <-stopped:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent Stop did not return")
		}
	}
	require.NoError(t, <-done)
}
