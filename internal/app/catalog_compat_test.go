package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/app/storage"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/formats"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	reconcilemocks "github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile/mocks"
)

// Tests in this file that call captureCompatLogs swap the default logger and
// must not be parallel: Go runs them before any parallel test resumes.

//nolint:paralleltest // Swaps the process-wide default logger.
func TestCatalogDefinitions_NormalizesV154Settings(t *testing.T) {
	const (
		repo     = "https://github.com/example/repo.git"
		endpoint = "https://r.example.com"
		fileURL  = "https://example.com/r.json"
		gitWarn  = "Git source sets more than one of branch, tag and commit; as in v1.5.4, every sync of it fails until only one is set"
	)
	hourly := &config.SyncPolicyConfig{Interval: "1h"}
	every := func(interval string) *config.SyncPolicyConfig { return &config.SyncPolicyConfig{Interval: interval} }
	excludeAll := &persistence.Filter{Names: &persistence.NameFilter{Exclude: []string{"*"}}}

	tests := []struct {
		name   string
		source config.SourceConfig
		// registry lists the registry's sources; nil means the source alone.
		registry []string
		// want is the source's definition; wantView the registry's sources.
		want     persistence.SourceDefinition
		wantView []string
		logMsg   string
		logLevel string
	}{
		{
			name:   "zero sync interval",
			source: config.SourceConfig{Name: "src", API: &config.APIConfig{Endpoint: endpoint}, SyncPolicy: every("0s")},
			want:   persistence.SourceDefinition{Name: "src", API: &persistence.APISpec{Endpoint: endpoint}, Schedule: "2m0s"},
			logMsg: "Normalized syncPolicy.interval",
		},
		{
			name:   "negative sync interval",
			source: config.SourceConfig{Name: "src", API: &config.APIConfig{Endpoint: endpoint}, SyncPolicy: every("-10m")},
			want:   persistence.SourceDefinition{Name: "src", API: &persistence.APISpec{Endpoint: endpoint}, Schedule: "2m0s"},
			logMsg: "Normalized syncPolicy.interval",
		},
		{
			name:   "sub-second sync interval",
			source: config.SourceConfig{Name: "src", File: &config.FileConfig{Path: "/data/r.json"}, SyncPolicy: every("250ms")},
			want:   persistence.SourceDefinition{Name: "src", File: &persistence.FileSpec{Path: "/data/r.json"}, Schedule: "1s"},
			logMsg: "Normalized syncPolicy.interval",
		},
		{
			name: "file timeout on a path source",
			source: config.SourceConfig{
				Name: "src", File: &config.FileConfig{Path: "/data/r.json", Timeout: "30s"}, SyncPolicy: hourly,
			},
			want:   persistence.SourceDefinition{Name: "src", File: &persistence.FileSpec{Path: "/data/r.json"}, Schedule: "1h"},
			logMsg: "Normalized file.timeout",
		},
		{
			name: "zero file timeout on a URL source",
			source: config.SourceConfig{
				Name: "src", File: &config.FileConfig{URL: fileURL, Timeout: "0s"}, SyncPolicy: hourly,
			},
			want: persistence.SourceDefinition{
				Name: "src", File: &persistence.FileSpec{URL: fileURL, Timeout: "10s"}, Schedule: "1h",
			},
			logMsg: "Normalized file.timeout",
		},
		{
			name: "negative file timeout on a URL source",
			source: config.SourceConfig{
				Name: "src", File: &config.FileConfig{URL: fileURL, Timeout: "-5s"}, SyncPolicy: hourly,
			},
			want:   persistence.SourceDefinition{Name: "src", File: &persistence.FileSpec{URL: fileURL}, Schedule: "1h"},
			logMsg: "Normalized file.timeout",
		},
		{
			name: "git branch and tag",
			source: config.SourceConfig{
				Name: "src", Git: &config.GitConfig{Repository: repo, Branch: "main", Tag: "v1"}, SyncPolicy: hourly,
			},
			want: persistence.SourceDefinition{
				Name: "src", Git: &persistence.GitSpec{Repository: repo, Branch: "main"}, Schedule: "1h",
			},
			logMsg: gitWarn,
		},
		{
			name: "git branch, tag and commit",
			source: config.SourceConfig{
				Name: "src", Git: &config.GitConfig{Repository: repo, Branch: "main", Tag: "v1", Commit: "abc123"},
				SyncPolicy: hourly,
			},
			want: persistence.SourceDefinition{
				Name: "src", Git: &persistence.GitSpec{Repository: repo, Commit: "abc123"}, Schedule: "1h",
			},
			logMsg: gitWarn,
		},
		{
			name:     "registry listing a source twice",
			source:   config.SourceConfig{Name: "src", Managed: &config.ManagedConfig{}},
			registry: []string{"src", "other", "src"},
			want:     persistence.SourceDefinition{Name: "src", Managed: &persistence.ManagedSpec{}},
			wantView: []string{"other", "src"}, // each source keeps its last position, as in v1.5.4
			logMsg:   "Registry lists a source more than once; as in v1.5.4, each source keeps the position of its last listing",
		},
		{
			name: "invalid include glob",
			source: config.SourceConfig{
				Name: "src", Git: &config.GitConfig{Repository: repo}, SyncPolicy: hourly,
				Filter: &config.FilterConfig{Names: &config.NameFilterConfig{Include: []string{"[unclosed"}}},
			},
			want: persistence.SourceDefinition{
				Name: "src", Git: &persistence.GitSpec{Repository: repo}, Schedule: "1h", Filter: excludeAll,
			},
			logMsg:   "Invalid name filter pattern",
			logLevel: "ERROR",
		},
		{
			name: "invalid exclude glob",
			source: config.SourceConfig{
				Name: "src", Git: &config.GitConfig{Repository: repo}, SyncPolicy: hourly,
				Filter: &config.FilterConfig{Names: &config.NameFilterConfig{Exclude: []string{"{a,b"}}},
			},
			want: persistence.SourceDefinition{
				Name: "src", Git: &persistence.GitSpec{Repository: repo}, Schedule: "1h", Filter: excludeAll,
			},
			logMsg:   "Invalid name filter pattern",
			logLevel: "ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureCompatLogs(t)
			registry, wantView := tt.registry, tt.wantView
			sources := []config.SourceConfig{tt.source}
			if registry == nil {
				registry, wantView = []string{tt.source.Name}, []string{tt.source.Name}
			} else {
				sources = append(sources, config.SourceConfig{Name: "other", File: &config.FileConfig{Data: validInlineRegistry}})
			}
			cfg := &config.Config{Sources: sources, Registries: []config.RegistryConfig{{Name: "main", Sources: registry}}}

			defs, views, err := catalogDefinitions(cfg)
			require.NoError(t, err, "a config v1.5.4 accepted must build definitions")
			assert.Equal(t, tt.want, defs[0])
			assert.Equal(t, wantView, views[0].Sources)

			records := logs.records(t, tt.logMsg)
			require.NotEmpty(t, records, "expected a %q log line", tt.logMsg)
			level := tt.logLevel
			if level == "" {
				level = "WARN"
			}
			assert.Equal(t, level, records[0]["level"])
			if tt.registry != nil {
				assert.Equal(t, "main", records[0]["registry"])
			} else {
				assert.Equal(t, tt.source.Name, records[0]["source"])
			}
		})
	}
}

func TestAmbiguousGitRefFetcher(t *testing.T) {
	t.Parallel()
	const repo = "https://github.com/example/repo.git"
	cfg := &config.Config{Sources: []config.SourceConfig{
		{Name: "ambiguous", Git: &config.GitConfig{Repository: repo, Branch: "main", Tag: "v1"}},
		{Name: "clear", Git: &config.GitConfig{Repository: repo, Branch: "main"}},
	}}
	stored := persistence.GitSpec{Repository: repo, Branch: "main"}

	tests := []struct {
		name    string
		def     persistence.SourceDefinition
		refused bool
	}{
		{name: "config source built from the ambiguous config", refused: true,
			def: persistence.SourceDefinition{Name: "ambiguous", Origin: persistence.OriginConfig, Git: &stored}},
		{name: "config source whose stored definition differs",
			def: persistence.SourceDefinition{Name: "ambiguous", Origin: persistence.OriginConfig,
				Git: &persistence.GitSpec{Repository: repo, Tag: "v1"}}},
		{name: "API source of the same name",
			def: persistence.SourceDefinition{Name: "ambiguous", Origin: persistence.OriginAPI, Git: &stored}},
		{name: "unambiguous config source",
			def: persistence.SourceDefinition{Name: "clear", Origin: persistence.OriginConfig, Git: &stored}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			raw := reconcilemocks.NewMockFetcher(gomock.NewController(t))
			if !tt.refused {
				raw.EXPECT().Fetch(gomock.Any(), tt.def).Return(ingest.Result{Hash: "h"}, nil)
			}
			result, err := withAmbiguousGitRefs(raw, cfg).Fetch(context.Background(), tt.def)
			if !tt.refused {
				require.NoError(t, err)
				assert.Equal(t, "h", result.Hash)
				return
			}
			require.ErrorIs(t, err, ingest.ErrFetch)
			assert.Equal(t, errAmbiguousGitRef.Error(), ingest.Cause(err))
		})
	}

	t.Run("no ambiguous source leaves the fetcher alone", func(t *testing.T) {
		t.Parallel()
		raw := reconcilemocks.NewMockFetcher(gomock.NewController(t))
		assert.Same(t, raw, withAmbiguousGitRefs(raw, &config.Config{Sources: cfg.Sources[1:]}))
	})
}

func TestConfigClaimsForStored(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Sources: []config.SourceConfig{
			{Name: "kept", File: &config.FileConfig{Path: "/r.json"}, Claims: map[string]any{"org": "a"}},
			{Name: "api-owned", File: &config.FileConfig{Path: "/r.json"}, Claims: map[string]any{"org": "b"}},
			{Name: "new", Managed: &config.ManagedConfig{}},
			{Name: "kind-changed", API: &config.APIConfig{Endpoint: "https://r.example.com"}},
		},
		Registries: []config.RegistryConfig{{Name: "main", Sources: []string{"kept"}}},
	}
	stored := []persistence.SourceDefinition{
		{Name: "kept", Origin: persistence.OriginConfig, File: &persistence.FileSpec{Path: "/r.json"}, Schedule: "1h"},
		{Name: "api-owned", Origin: persistence.OriginAPI, File: &persistence.FileSpec{Path: "/r.json"}, Schedule: "1h"},
		{Name: "kind-changed", Origin: persistence.OriginConfig, File: &persistence.FileSpec{Path: "/r.json"}, Schedule: "1h"},
	}

	got := configClaimsForStored(cfg, stored)
	names := make([]string, 0, len(got.Sources))
	for _, s := range got.Sources {
		names = append(names, s.Name)
	}
	assert.Equal(t, []string{"kept"}, names)
	assert.Equal(t, cfg.Registries, got.Registries)
	assert.Len(t, cfg.Sources, 4, "the input config is not modified")
}

// TestNewRegistryApp_StartsWithV154Config loads a YAML config with every
// setting v1.5.4 accepted that the library refuses, and checks that the app
// starts, stores the normalized definitions and syncs every source except the
// git source with more than one ref, whose syncs fail as they did in v1.5.4.
func TestNewRegistryApp_StartsWithV154Config(t *testing.T) {
	t.Parallel()
	db := newTestDatabaseConfig(t)
	yaml := fmt.Sprintf(`
auth:
  mode: anonymous
database:
  host: %q
  port: %d
  user: %q
  password: %q
  database: %q
  sslMode: disable
sources:
  - name: zero-interval
    api:
      endpoint: https://registry.example.com
    syncPolicy:
      interval: 0s
  - name: fast
    file:
      url: https://example.com/fast.json
      timeout: 0s
    syncPolicy:
      interval: 100ms
  - name: path-timeout
    file:
      path: /data/registry.json
      timeout: 30s
    syncPolicy:
      interval: 1h
  - name: ambiguous-git
    git:
      repository: https://github.com/example/repo.git
      branch: main
      tag: v1.0.0
    syncPolicy:
      interval: 1h
  - name: bad-glob
    git:
      repository: https://github.com/example/repo.git
    syncPolicy:
      interval: 1h
    filter:
      names:
        include: ["[unclosed"]
registries:
  - name: main
    sources: [fast, zero-interval, fast, path-timeout, ambiguous-git, bad-glob]
`, db.Host, db.Port, db.User, db.Password, db.Database)
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))
	cfg, err := config.LoadConfig(config.WithConfigPath(path))
	require.NoError(t, err, "v1.5.4 accepted this config")

	snapshot, err := formats.DecodeUpstream([]byte(validInlineRegistry))
	require.NoError(t, err)
	var mu sync.Mutex
	fetched := map[string]bool{}
	fetcher := reconcilemocks.NewMockFetcher(gomock.NewController(t))
	fetcher.EXPECT().Fetch(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, d persistence.SourceDefinition) (ingest.Result, error) {
			mu.Lock()
			fetched[d.Name] = true
			mu.Unlock()
			return ingest.Result{Snapshot: *snapshot, Hash: d.Name}, nil
		}).AnyTimes()

	app := newTestRegistryApp(t, WithConfig(cfg), WithReconcileOptions(fastReconcile), WithFetcher(fetcher))
	cat := openTestCatalog(t, db)
	ctx := context.Background()

	stored := map[string]persistence.SourceDefinition{}
	defs, err := cat.Definitions.ListSources(ctx)
	require.NoError(t, err)
	for _, d := range defs {
		stored[d.Name] = d
	}
	assert.Equal(t, "2m0s", stored["zero-interval"].Schedule)
	assert.Equal(t, "1s", stored["fast"].Schedule)
	assert.Equal(t, "10s", stored["fast"].File.Timeout)
	assert.Empty(t, stored["path-timeout"].File.Timeout)
	assert.Equal(t, "main", stored["ambiguous-git"].Git.Branch)
	assert.Empty(t, stored["ambiguous-git"].Git.Tag)
	assert.Equal(t, &persistence.Filter{Names: &persistence.NameFilter{Exclude: []string{"*"}}}, stored["bad-glob"].Filter)
	view, err := cat.Definitions.GetView(ctx, "main")
	require.NoError(t, err)
	assert.Equal(t, []string{"zero-interval", "fast", "path-timeout", "ambiguous-git", "bad-glob"}, view.Sources)

	runSync(t, app.components.Sync)
	status := func(name string) string {
		var s string
		err := cat.Pool.QueryRow(ctx, `SELECT rs.sync_status::text FROM registry_sync rs
			JOIN source s ON s.id = rs.source_id WHERE s.name = $1`, name).Scan(&s)
		if err != nil {
			return ""
		}
		return s
	}
	require.Eventually(t, func() bool {
		for _, name := range []string{"zero-interval", "fast", "path-timeout", "bad-glob"} {
			if status(name) != "COMPLETED" {
				return false
			}
		}
		return status("ambiguous-git") == "FAILED"
	}, 15*time.Second, 50*time.Millisecond, "every source but the ambiguous git source syncs")
	mu.Lock()
	assert.False(t, fetched["ambiguous-git"], "the ambiguous git source is never fetched")
	mu.Unlock()

	// The bad-glob source excluded everything, as v1.5.4 did; the others serve.
	var badGlobEntries int
	require.NoError(t, cat.Pool.QueryRow(ctx, `SELECT count(*) FROM registry_entry e
		JOIN source s ON s.id = e.source_id WHERE s.name = 'bad-glob'`).Scan(&badGlobEntries))
	assert.Zero(t, badGlobEntries)
	rec := httptest.NewRecorder()
	app.GetHTTPServer().Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/registry/main/v0.1/servers", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "io.example/weather")
}

// conflictCase seeds a database, then starts the app with a config that
// conflicts with what the database holds.
type conflictCase struct {
	name string
	// seed prepares the database through the library, before the app starts.
	seed func(t *testing.T, cat *storage.Catalog)
	cfg  *config.Config
	// conflict holds strings the logged error must contain.
	conflict []string
	// served is a registry the app must keep serving from stored definitions,
	// with entries synced by this app.
	served string
	// notApplied is a config registry that must not exist after the start.
	notApplied string
}

//nolint:paralleltest // Swaps the process-wide default logger.
func TestNewRegistryApp_ConfigConflictsKeepStoredDefinitions(t *testing.T) {
	inline := func(name string) config.SourceConfig {
		return config.SourceConfig{Name: name, File: &config.FileConfig{Data: validInlineRegistry}}
	}
	inlineDef := func(name string) persistence.SourceDefinition {
		return persistence.SourceDefinition{Name: name, File: &persistence.FileSpec{Data: validInlineRegistry}}
	}
	anonymous := &config.AuthConfig{Mode: config.AuthModeAnonymous}

	tests := []conflictCase{
		{
			name: "config source named like an API-created source",
			seed: func(t *testing.T, cat *storage.Catalog) {
				t.Helper()
				ctx := context.Background()
				_, err := cat.Definitions.CreateSource(ctx, inlineDef("shared"))
				require.NoError(t, err)
				_, err = cat.Definitions.CreateView(ctx, persistence.ViewDefinition{Name: "api-reg", Sources: []string{"shared"}})
				require.NoError(t, err)
			},
			cfg: &config.Config{
				Auth: anonymous,
				Sources: []config.SourceConfig{
					{Name: "shared", File: &config.FileConfig{Data: validInlineRegistry}, Claims: map[string]any{"org": "acme"}},
				},
				Registries: []config.RegistryConfig{{Name: "main", Sources: []string{"shared"}}},
			},
			conflict:   []string{`source "shared" is API-created`},
			served:     "api-reg",
			notApplied: "main",
		},
		{
			name: "config drops a source an API-created registry uses",
			seed: func(t *testing.T, cat *storage.Catalog) {
				t.Helper()
				ctx := context.Background()
				require.NoError(t, cat.Definitions.Reconcile(ctx,
					[]persistence.SourceDefinition{inlineDef("cfg-inline")},
					[]persistence.ViewDefinition{{Name: "main", Sources: []string{"cfg-inline"}}}))
				_, err := cat.Definitions.CreateView(ctx, persistence.ViewDefinition{Name: "api-reg", Sources: []string{"cfg-inline"}})
				require.NoError(t, err)
			},
			cfg: &config.Config{
				Auth:       anonymous,
				Sources:    []config.SourceConfig{inline("cfg-other")},
				Registries: []config.RegistryConfig{{Name: "next", Sources: []string{"cfg-other"}}},
			},
			conflict:   []string{`source "cfg-inline"`, `registry "api-reg"`},
			served:     "main",
			notApplied: "next",
		},
		{
			name: "config managed source beside an API-created managed source",
			seed: func(t *testing.T, cat *storage.Catalog) {
				t.Helper()
				ctx := context.Background()
				_, err := cat.Definitions.CreateSource(ctx, persistence.SourceDefinition{
					Name: "api-managed", Managed: &persistence.ManagedSpec{},
				})
				require.NoError(t, err)
				_, err = cat.Definitions.CreateSource(ctx, inlineDef("api-inline"))
				require.NoError(t, err)
				_, err = cat.Definitions.CreateView(ctx, persistence.ViewDefinition{
					Name: "api-reg", Sources: []string{"api-inline", "api-managed"},
				})
				require.NoError(t, err)
			},
			cfg: &config.Config{
				Auth:       anonymous,
				Sources:    []config.SourceConfig{inline("cfg-inline"), {Name: "cfg-managed", Managed: &config.ManagedConfig{}}},
				Registries: []config.RegistryConfig{{Name: "main", Sources: []string{"cfg-inline", "cfg-managed"}}},
			},
			conflict:   []string{`managed source "cfg-managed"`, `"api-managed"`},
			served:     "api-reg",
			notApplied: "main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newTestDatabaseConfig(t)
			cat := openTestCatalog(t, db)
			tt.seed(t, cat)
			tt.cfg.Database = db

			logs := captureCompatLogs(t)
			app := newTestRegistryApp(t, WithConfig(tt.cfg), WithReconcileOptions(fastReconcile))

			records := logs.records(t, "Config sources and registries were not applied because they conflict "+
				"with the database; serving and syncing the stored definitions until the conflict is resolved")
			require.Len(t, records, 1, "the conflict is logged once")
			assert.Equal(t, "ERROR", records[0]["level"])
			for _, want := range tt.conflict {
				assert.Contains(t, records[0]["error"], want)
			}

			_, err := cat.Definitions.GetView(context.Background(), tt.notApplied)
			assert.ErrorIs(t, err, persistence.ErrNotFound, "the conflicting config is not applied")

			runSync(t, app.components.Sync)
			require.Eventually(t, func() bool {
				rec := httptest.NewRecorder()
				app.GetHTTPServer().Handler.ServeHTTP(rec,
					httptest.NewRequest(http.MethodGet, "/registry/"+tt.served+"/v0.1/servers", nil))
				return rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "io.example/weather")
			}, 15*time.Second, 50*time.Millisecond, "stored definitions keep syncing and serving")
			rec := httptest.NewRecorder()
			app.GetInternalHTTPServer().Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readiness", nil))
			assert.Equal(t, http.StatusOK, rec.Code, "the server reports ready")
		})
	}
}

// TestNewRegistryApp_ConflictDoesNotApplyConfigClaimsToAPISource checks that
// a config source named like an API-created source never gives that source
// or its entries the config's claims.
func TestNewRegistryApp_ConflictDoesNotApplyConfigClaimsToAPISource(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDatabaseConfig(t)
	cat := openTestCatalog(t, db)
	_, err := cat.Definitions.CreateSource(ctx, persistence.SourceDefinition{
		Name: "shared", File: &persistence.FileSpec{Path: "/data/r.json"}, Schedule: "1h",
	})
	require.NoError(t, err)
	var sourceID string
	require.NoError(t, cat.Pool.QueryRow(ctx, `SELECT id::text FROM source WHERE name = 'shared'`).Scan(&sourceID))
	_, err = cat.Pool.Exec(ctx, `INSERT INTO registry_entry (source_id, entry_type, name, claims)
		VALUES ($1, 'MCP', 'io.example/weather', NULL)`, sourceID)
	require.NoError(t, err)

	cfg := &config.Config{
		Database: db,
		Auth:     &config.AuthConfig{Mode: config.AuthModeAnonymous},
		Sources: []config.SourceConfig{{
			Name: "shared", File: &config.FileConfig{Path: "/data/r.json"},
			SyncPolicy: &config.SyncPolicyConfig{Interval: "1h"}, Claims: map[string]any{"org": "acme"},
		}},
		Registries: []config.RegistryConfig{{Name: "main", Sources: []string{"shared"}}},
	}
	newTestRegistryApp(t, WithConfig(cfg), WithReconcileOptions(fastReconcile))

	var sourceClaims, entryClaims *string
	require.NoError(t, cat.Pool.QueryRow(ctx, `SELECT claims::text FROM source WHERE name = 'shared'`).Scan(&sourceClaims))
	require.NoError(t, cat.Pool.QueryRow(ctx, `SELECT claims::text FROM registry_entry WHERE source_id = $1`,
		sourceID).Scan(&entryClaims))
	assert.Nil(t, sourceClaims, "the API-created source keeps its claims")
	assert.Nil(t, entryClaims, "its entries keep their claims")
}

// TestNewRegistryApp_AuthFailureLeavesDatabaseUnchanged checks that auth is
// built before the first database write: a start that fails on auth config
// stores no sources, registries or claims.
func TestNewRegistryApp_AuthFailureLeavesDatabaseUnchanged(t *testing.T) {
	t.Parallel()
	db := newTestDatabaseConfig(t)
	cfg := catalogTestConfig(db)
	cfg.Auth = &config.AuthConfig{Mode: "unsupported"}

	_, err := NewRegistryApp(context.Background(), WithConfig(cfg), WithReconcileOptions(fastReconcile))
	require.ErrorContains(t, err, "failed to build auth middleware")

	cat := openTestCatalog(t, db)
	sources, err := cat.Definitions.ListSources(context.Background())
	require.NoError(t, err)
	assert.Empty(t, sources, "nothing was written before auth failed")
	views, err := cat.Definitions.ListViews(context.Background())
	require.NoError(t, err)
	assert.Empty(t, views)
}

func TestReconcileConfig_StorageFailureIsFatal(t *testing.T) {
	t.Parallel()
	assert.False(t, isConfigConflict(fmt.Errorf("wrapped: %w", persistence.ErrContention)))
	assert.False(t, isConfigConflict(persistence.ErrUnavailable))
	assert.False(t, isConfigConflict(persistence.ErrInvalid))
	assert.False(t, isConfigConflict(context.Canceled))
	assert.True(t, isConfigConflict(fmt.Errorf("%w: source \"x\" is API-created", persistence.ErrConflict)))
	assert.True(t, isConfigConflict(fmt.Errorf("%w: source \"x\" cannot change", persistence.ErrKindChange)))
	assert.True(t, isConfigConflict(errors.Join(persistence.ErrInUse)))
}

// openTestCatalog opens the library adapters over an existing test database.
func openTestCatalog(t *testing.T, db *config.DatabaseConfig) *storage.Catalog {
	t.Helper()
	ctx := context.Background()
	factory, err := storage.NewDatabaseFactory(ctx, &config.Config{Database: db})
	require.NoError(t, err)
	t.Cleanup(factory.Cleanup)
	cat, err := factory.CreateCatalog(ctx, nil)
	require.NoError(t, err)
	return cat
}

// compatLogs collects JSON log output written from several goroutines.
type compatLogs struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *compatLogs) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// records returns the logged records with the given message.
func (b *compatLogs) records(t *testing.T, msg string) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(b.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &record))
		if record["msg"] == msg {
			out = append(out, record)
		}
	}
	return out
}

// captureCompatLogs swaps the default logger for the test's duration.
func captureCompatLogs(t *testing.T) *compatLogs {
	t.Helper()
	logs := &compatLogs{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return logs
}
