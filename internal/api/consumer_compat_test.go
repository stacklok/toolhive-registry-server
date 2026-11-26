package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/api"
	auditmw "github.com/stacklok/toolhive-registry-server/internal/audit"
	"github.com/stacklok/toolhive-registry-server/internal/claims"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/service/mocks"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// stubPages serves fixed entries, or err, for every consumer read; lists
// carry cursor as their continuation.
type stubPages struct {
	entries []persistence.Entry
	err     error
	cursor  string
}

func (p stubPages) List(context.Context, string, catalog.Query) (catalog.Page, error) {
	return catalog.Page{Entries: p.clone(), NextCursor: p.cursor}, p.err
}

func (p stubPages) Get(context.Context, string, catalog.EntryName, catalog.VersionSelector) (persistence.Entry, error) {
	if p.err != nil || len(p.entries) == 0 {
		return persistence.Entry{}, errors.Join(p.err, persistence.ErrNotFound)
	}
	return p.clone()[0], nil
}

func (p stubPages) ListVersions(context.Context, string, catalog.EntryName, catalog.PageRequest) (catalog.Page, error) {
	return catalog.Page{Entries: p.clone()}, p.err
}

func (p stubPages) clone() []persistence.Entry {
	out := make([]persistence.Entry, len(p.entries))
	for i, e := range p.entries {
		if e.Skill != nil {
			s := *e.Skill
			e.Skill = &s
		}
		if e.Server != nil {
			s := *e.Server
			e.Server = &s
		}
		out[i] = e
	}
	return out
}

func serve(t *testing.T, pages stubPages, managed api.ManagedSourceIDs, path string) (int, map[string]any) {
	t.Helper()
	svc := mocks.NewMockRegistryService(gomock.NewController(t))
	server, err := api.NewServer(svc, pages, api.WithManagedSourceIDs(managed))
	require.NoError(t, err)
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body), rr.Body.String())
	return rr.Code, body
}

func TestConsumerKeepsV154Shapes(t *testing.T) {
	t.Parallel()
	managed := func(context.Context) (map[string]bool, error) { return map[string]bool{"mgd": true}, nil }
	skill := func(source string) persistence.Entry {
		return persistence.Entry{SourceID: source, Kind: persistence.SkillKind, Name: "s", Version: "1.0.0", IsLatest: true,
			Skill: &model.Skill{Namespace: "io.test", Name: "s", Version: "1.0.0", Description: "d", Status: "active"}}
	}
	_, body := serve(t, stubPages{entries: []persistence.Entry{skill("mgd"), skill("synced")}}, managed,
		"/registry/main/v0.1/x/dev.toolhive/skills")
	items := body["skills"].([]any)
	require.Len(t, items, 2)
	first, second := items[0].(map[string]any), items[1].(map[string]any)
	assert.Equal(t, "ACTIVE", first["status"], "status keeps v1.5.4's stored case")
	assert.Equal(t, map[string]any{}, first["repository"], "managed skills always carry repository")
	assert.NotContains(t, second, "repository", "synced skills without a repository omit it")

	server := persistence.Entry{SourceID: "synced", Kind: persistence.ServerKind, Name: "io.test/srv", Version: "1.0.0",
		Server: &model.Server{Schema: "https://example.org/s.json", Name: "io.test/srv", Version: "1.0.0", Description: "d"}}
	_, body = serve(t, stubPages{entries: []persistence.Entry{server}}, managed, "/registry/main/v0.1/servers")
	got := body["servers"].([]any)[0].(map[string]any)["server"].(map[string]any)
	assert.Equal(t, map[string]any{}, got["_meta"], "servers always carry _meta")
}

func TestConsumerRegistryForbidden(t *testing.T) {
	t.Parallel()
	code, body := serve(t, stubPages{err: claims.ErrRegistryForbidden}, nil, "/registry/main/v0.1/servers")
	assert.Equal(t, http.StatusForbidden, code)
	assert.Equal(t, "forbidden: insufficient claims for registry", body["error"])

	code, body = serve(t, stubPages{err: persistence.ErrNotFound}, nil, "/registry/main/v0.1/servers")
	assert.Equal(t, http.StatusNotFound, code)
	assert.Equal(t, "not found", body["error"])

	// A missing registry keeps v1.5.4's message on every consumer route.
	for _, path := range []string{
		"/registry/main/v0.1/servers",
		"/registry/main/v0.1/servers/io.test%2Fsrv/versions",
		"/registry/main/v0.1/servers/io.test%2Fsrv/versions/latest",
		"/registry/main/v0.1/x/dev.toolhive/skills",
		"/registry/main/v0.1/x/dev.toolhive/plugins",
	} {
		code, body = serve(t, stubPages{err: persistence.ErrViewNotFound}, nil, path)
		assert.Equal(t, http.StatusNotFound, code, path)
		assert.Equal(t, "registry not found", body["error"], path)
	}
}

func TestConsumerManagedLookupFailure(t *testing.T) {
	t.Parallel()
	failing := func(context.Context) (map[string]bool, error) { return nil, errors.New("db down") }
	entry := persistence.Entry{SourceID: "x", Kind: persistence.SkillKind, Name: "s", Version: "1",
		Skill: &model.Skill{Namespace: "io.test", Name: "s", Version: "1", Description: "d"}}
	code, _ := serve(t, stubPages{entries: []persistence.Entry{entry}}, failing, "/registry/main/v0.1/x/dev.toolhive/skills")
	assert.Equal(t, http.StatusInternalServerError, code)
}

func TestConsumerReadsAreAudited(t *testing.T) {
	t.Parallel()
	logFile := filepath.Join(t.TempDir(), "audit.log")
	logger, err := auditmw.NewLogger(logFile)
	require.NoError(t, err)
	t.Cleanup(func() { _ = logger.Close() })

	svc := mocks.NewMockRegistryService(gomock.NewController(t))
	server, err := api.NewServer(svc, stubPages{err: persistence.ErrNotFound},
		api.WithMiddlewares(auditmw.Middleware(&config.AuditConfig{Enabled: true}, logger)))
	require.NoError(t, err)
	for _, path := range []string{
		"/registry/main/v0.1/servers",
		"/registry/main/v0.1/servers/io.test%2Fsrv/versions/1.0.0",
		"/registry/main/v0.1/x/dev.toolhive/skills/io.test/s",
		"/registry/main/v0.1/x/dev.toolhive/plugins/io.test/p/versions",
	} {
		server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}
	require.NoError(t, logger.Close())
	data, err := os.ReadFile(logFile)
	require.NoError(t, err)
	for _, event := range []string{"server.list", "server.version.read", "skill.read", "plugin.versions.list"} {
		assert.Contains(t, string(data), `"`+event+`"`, "audit event %s", event)
	}
}
