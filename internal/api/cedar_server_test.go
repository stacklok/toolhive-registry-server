package api_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/api"
	auditmw "github.com/stacklok/toolhive-registry-server/internal/audit"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/cedar"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// cedarTestPolicy lets anyone read the "public" view, authenticated callers
// read the OpenAPI document, and only the "admin" subject read and write
// sources.
const cedarTestPolicy = `
permit(principal, action == Registry::Action::"catalog.read", resource == Registry::View::"public");
permit(principal, action == Registry::Action::"metadata.read", resource) when { principal.authenticated };
permit(
  principal,
  action in [Registry::Action::"sources.read", Registry::Action::"sources.write"],
  resource == Registry::Instance::"catalog"
) when { principal.subject == "admin" };
`

const (
	testUserHeader = "X-Test-User"
	testIssuer     = "https://idp.example.com"
	deniedBody     = `{"error":"authorization denied or unavailable"}` + "\n"
)

// testIdentity is the caller named by X-Test-User, or anonymous without it.
func testIdentity(r *http.Request) (cedar.Identity, bool) {
	user := r.Header.Get(testUserHeader)
	if user == "" {
		return cedar.Identity{Anonymous: true}, true
	}
	return cedar.Identity{Issuer: testIssuer, Subject: user, Claims: map[string]any{"iss": testIssuer, "sub": user}}, true
}

func newTestAuthorizer(t *testing.T) *cedar.Authorizer {
	t.Helper()
	engine, err := cedar.Parse([]byte(cedarTestPolicy))
	require.NoError(t, err)
	authz, err := cedar.NewAuthorizer(engine, cedar.Options{Identity: testIdentity})
	require.NoError(t, err)
	return authz
}

// fakeSources is an in-memory persistence.Sources. createErr, when set, is
// returned from CreateSource after the source is stored.
type fakeSources struct {
	mu        sync.Mutex
	byName    map[string]persistence.SourceDefinition
	createErr error
}

func newFakeSources(names ...string) *fakeSources {
	s := &fakeSources{byName: map[string]persistence.SourceDefinition{}}
	for _, n := range names {
		s.byName[n] = persistence.SourceDefinition{ID: "id-" + n, Name: n, Managed: &persistence.ManagedSpec{}}
	}
	return s
}

func (s *fakeSources) CreateSource(_ context.Context, d persistence.SourceDefinition) (persistence.SourceDefinition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byName[d.Name]; ok {
		return persistence.SourceDefinition{}, persistence.ErrAlreadyExists
	}
	d.ID = "id-" + d.Name
	s.byName[d.Name] = d
	return d, s.createErr
}

func (s *fakeSources) GetSource(_ context.Context, name string) (persistence.SourceDefinition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.byName[name]
	if !ok {
		return d, persistence.ErrNotFound
	}
	return d, nil
}

func (s *fakeSources) ListSources(context.Context) ([]persistence.SourceDefinition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]persistence.SourceDefinition, 0, len(s.byName))
	for _, d := range s.byName {
		out = append(out, d)
	}
	return out, nil
}

func (s *fakeSources) UpdateSource(_ context.Context, name string, d persistence.SourceDefinition) (persistence.SourceDefinition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.byName[name]
	if !ok {
		return d, persistence.ErrNotFound
	}
	d.ID = existing.ID
	s.byName[name] = d
	return d, nil
}

func (s *fakeSources) DeleteSource(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byName, name)
	return nil
}

// fakeViews, fakePublisher and fakeDiagnostics satisfy the remaining admin
// capabilities; the tests never reach their mutations.
type fakeViews struct{ persistence.Views }

func (fakeViews) ListViews(context.Context) ([]persistence.ViewDefinition, error) { return nil, nil }

type fakePublisher struct{ persistence.ManagedPublisher }

type fakeDiagnostics struct{}

func (fakeDiagnostics) ListSourceVersions(context.Context, string) ([]persistence.DiagnosticVersion, error) {
	return nil, nil
}

func (fakeDiagnostics) ListViewVersions(context.Context, string) ([]persistence.ViewDiagnosticVersion, error) {
	return nil, nil
}

func (fakeDiagnostics) Status(_ context.Context, name string) (persistence.JobStatus, error) {
	return persistence.JobStatus{SourceID: "id-" + name}, nil
}

func newTestAdmin(sources *fakeSources) *httpapi.AdminOperations {
	return &httpapi.AdminOperations{
		Sources: sources, Views: fakeViews{}, Publisher: fakePublisher{}, Diagnostics: fakeDiagnostics{},
	}
}

func publicServerPages() stubPages {
	return stubPages{entries: []persistence.Entry{{
		SourceID: "synced", Kind: persistence.ServerKind, Name: "io.test/srv", Version: "1.0.0", IsLatest: true,
		Server: &model.Server{Name: "io.test/srv", Version: "1.0.0", Description: "d"},
	}}}
}

func doRequest(handler http.Handler, method, path, user, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if user != "" {
		req.Header.Set(testUserHeader, user)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestNewCedarServer_Routes(t *testing.T) {
	t.Parallel()

	wellKnown := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resource":"https://registry.example.com"}`))
	})

	tests := []struct {
		name       string
		withAdmin  bool
		wellKnown  bool
		method     string
		path       string
		user       string
		body       string
		wantStatus int
		wantBody   string
		wantHeader map[string]string
	}{
		// Consumer routes: catalog.read on the requested view.
		{name: "permitted view is served to anonymous", method: http.MethodGet,
			path: "/registry/public/v0.1/servers", wantStatus: http.StatusOK},
		{name: "permitted view is served to a user", method: http.MethodGet,
			path: "/registry/public/v0.1/servers", user: "alice", wantStatus: http.StatusOK},
		{name: "unpermitted view is forbidden", method: http.MethodGet,
			path: "/registry/private/v0.1/servers", user: "alice", wantStatus: http.StatusForbidden, wantBody: deniedBody},
		{name: "unpermitted view is forbidden to anonymous", method: http.MethodGet,
			path: "/registry/private/v0.1/servers/io.test%2Fsrv/versions/latest", wantStatus: http.StatusForbidden, wantBody: deniedBody},
		{name: "skills extension follows the view policy", method: http.MethodGet,
			path: "/registry/private/v0.1/x/dev.toolhive/skills", user: "alice", wantStatus: http.StatusForbidden, wantBody: deniedBody},

		// /openapi.json is covered by TestNewCedarServer_OpenAPIRequiresMetadataRead.

		// Protected resource metadata is open.
		{name: "well-known served without authorization", wellKnown: true, method: http.MethodGet,
			path: "/.well-known/oauth-protected-resource", wantStatus: http.StatusOK,
			wantBody: `{"resource":"https://registry.example.com"}`},
		{name: "well-known absent without a handler", method: http.MethodGet,
			path: "/.well-known/oauth-protected-resource", wantStatus: http.StatusNotFound},

		// /v1 exists only with admin.
		{name: "admin routes absent without admin", method: http.MethodGet,
			path: "/v1/sources", user: "admin", wantStatus: http.StatusNotFound},
		{name: "admin upsert absent without admin", method: http.MethodPut,
			path: "/v1/sources/new", user: "admin", body: `{"managed":{}}`, wantStatus: http.StatusNotFound},
		{name: "admin routes mounted with admin", withAdmin: true, method: http.MethodGet,
			path: "/v1/sources", user: "admin", wantStatus: http.StatusOK},
		{name: "admin route denied by policy", withAdmin: true, method: http.MethodGet,
			path: "/v1/sources", user: "alice", wantStatus: http.StatusForbidden, wantBody: deniedBody},
		{name: "admin action not granted to admin", withAdmin: true, method: http.MethodDelete,
			path: "/v1/sources/existing", user: "admin", wantStatus: http.StatusForbidden, wantBody: deniedBody},
		{name: "anonymous on an admin route is unauthorized", withAdmin: true, method: http.MethodGet,
			path: "/v1/sources", wantStatus: http.StatusUnauthorized,
			wantBody: `{"error":"unauthorized"}` + "\n", wantHeader: map[string]string{"WWW-Authenticate": "Bearer"}},

		// Claims-mode-only endpoints do not exist in Cedar mode.
		{name: "/v1/me does not exist", withAdmin: true, method: http.MethodGet,
			path: "/v1/me", user: "admin", wantStatus: http.StatusNotFound},
		{name: "entry claims read does not exist", withAdmin: true, method: http.MethodGet,
			path: "/v1/entries/server/io.test%2Fsrv/claims", user: "admin", wantStatus: http.StatusNotFound},
		{name: "entry claims update does not exist", withAdmin: true, method: http.MethodPut,
			path: "/v1/entries/server/io.test%2Fsrv/claims", user: "admin", body: `{"claims":{}}`,
			wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var admin *httpapi.AdminOperations
			if tt.withAdmin {
				admin = newTestAdmin(newFakeSources("existing"))
			}
			var opts []api.ServerOption
			if tt.wellKnown {
				opts = append(opts, api.WithAuthInfoHandler(wellKnown))
			}
			server, err := api.NewCedarServer(publicServerPages(), admin, newTestAuthorizer(t), opts...)
			require.NoError(t, err)

			rec := doRequest(server, tt.method, tt.path, tt.user, tt.body)
			assert.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
			if tt.wantBody != "" {
				assert.Equal(t, tt.wantBody, rec.Body.String())
			}
			for k, v := range tt.wantHeader {
				assert.Equal(t, v, rec.Header().Get(k), "header %s", k)
			}
		})
	}
}

func TestNewCedarServer_ConsumerBodyServed(t *testing.T) {
	t.Parallel()
	server, err := api.NewCedarServer(publicServerPages(), nil, newTestAuthorizer(t))
	require.NoError(t, err)

	rec := doRequest(server, http.MethodGet, "/registry/public/v0.1/servers", "", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	servers := body["servers"].([]any)
	require.Len(t, servers, 1)
	server0 := servers[0].(map[string]any)["server"].(map[string]any)
	assert.Equal(t, "io.test/srv", server0["name"])
	assert.Equal(t, map[string]any{}, server0["_meta"], "Cedar mode keeps the v1.5.4 consumer shapes")
}

func TestNewCedarServer_MiddlewaresRun(t *testing.T) {
	t.Parallel()
	var calls int
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			next.ServeHTTP(w, r)
		})
	}
	server, err := api.NewCedarServer(publicServerPages(), nil, newTestAuthorizer(t), api.WithMiddlewares(mw))
	require.NoError(t, err)
	doRequest(server, http.MethodGet, "/registry/public/v0.1/servers", "", "")
	doRequest(server, http.MethodGet, "/registry/private/v0.1/servers", "", "")
	assert.Equal(t, 2, calls, "host middleware runs for allowed and denied requests")
}

// TestNewCedarServer_OpenAPIRequiresMetadataRead is not parallel: serving
// /openapi.json concurrently races inside swag.ReadDoc (see
// TestOpenAPIEndpoint_ConcurrentRequests).
//
//nolint:paralleltest // see above
func TestNewCedarServer_OpenAPIRequiresMetadataRead(t *testing.T) {
	server, err := api.NewCedarServer(publicServerPages(), nil, newTestAuthorizer(t))
	require.NoError(t, err)

	rec := doRequest(server, http.MethodGet, "/openapi.json", "", "")
	assert.Equal(t, http.StatusForbidden, rec.Code, "anonymous callers are not granted metadata.read")
	assert.Equal(t, deniedBody, rec.Body.String())

	rec = doRequest(server, http.MethodGet, "/openapi.json", "alice", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))
	assert.Contains(t, doc, "paths")
}

// auditEvents returns the audit events written to logFile, keyed by order.
func auditEvents(t *testing.T, logFile string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(logFile) //nolint:gosec // path is test-controlled
	require.NoError(t, err)
	var events []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		var event map[string]any
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &event), scanner.Text())
		events = append(events, event)
	}
	require.NoError(t, scanner.Err())
	return events
}

func eventExtra(t *testing.T, event map[string]any) map[string]any {
	t.Helper()
	metadata, ok := event["metadata"].(map[string]any)
	require.True(t, ok, "event has metadata: %v", event)
	extra, ok := metadata["extra"].(map[string]any)
	require.True(t, ok, "event has metadata.extra: %v", event)
	return extra
}

func newAuditedCedarServer(
	t *testing.T, sources *fakeSources,
) (http.Handler, func() []map[string]any) {
	t.Helper()
	logFile := filepath.Join(t.TempDir(), "audit.log")
	logger, err := auditmw.NewLogger(logFile)
	require.NoError(t, err)
	t.Cleanup(func() { _ = logger.Close() })
	server, err := api.NewCedarServer(publicServerPages(), newTestAdmin(sources), newTestAuthorizer(t),
		api.WithMiddlewares(auditmw.Middleware(&config.AuditConfig{Enabled: true}, logger)))
	require.NoError(t, err)
	return server, func() []map[string]any {
		require.NoError(t, logger.Close())
		return auditEvents(t, logFile)
	}
}

func TestNewCedarServer_AuditsAdminUpsertFromMutationResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		path          string
		createErr     error
		wantStatus    int
		wantEvent     string
		wantCommitted bool
	}{
		{name: "new source is a create", path: "/v1/sources/new",
			wantStatus: http.StatusCreated, wantEvent: "source.create", wantCommitted: true},
		{name: "existing source is an update", path: "/v1/sources/existing",
			wantStatus: http.StatusOK, wantEvent: "source.update", wantCommitted: true},
		// The create committed but follow-up work failed: the response is 503,
		// yet the event is still the create, marked committed.
		{name: "post-commit failure is still a committed create", path: "/v1/sources/new",
			createErr:  &catalog.PostCommitError{Cause: context.DeadlineExceeded},
			wantStatus: http.StatusServiceUnavailable, wantEvent: "source.create", wantCommitted: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sources := newFakeSources("existing")
			sources.createErr = tt.createErr
			server, events := newAuditedCedarServer(t, sources)

			rec := doRequest(server, http.MethodPut, tt.path, "admin", `{"managed":{}}`)
			require.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())

			logged := events()
			require.Len(t, logged, 1)
			assert.Equal(t, tt.wantEvent, logged[0]["type"])
			extra := eventExtra(t, logged[0])
			assert.Equal(t, tt.wantCommitted, extra["mutation_committed"])
			target := logged[0]["target"].(map[string]any)
			assert.Equal(t, "source", target["resource_type"])
		})
	}
}

func TestNewCedarServer_AuditsUncommittedAdminWithoutMarker(t *testing.T) {
	t.Parallel()
	server, events := newAuditedCedarServer(t, newFakeSources())

	// Allowed read: no mutation, so no mutation_committed marker.
	rec := doRequest(server, http.MethodGet, "/v1/sources", "admin", "")
	require.Equal(t, http.StatusOK, rec.Code)
	// Denied upsert: the library never runs, so nothing committed.
	rec = doRequest(server, http.MethodPut, "/v1/sources/new", "alice", `{"managed":{}}`)
	require.Equal(t, http.StatusForbidden, rec.Code)

	logged := events()
	require.Len(t, logged, 2)
	assert.Equal(t, "source.list", logged[0]["type"])
	assert.Equal(t, "success", logged[0]["outcome"])
	assert.NotContains(t, eventExtra(t, logged[0]), "mutation_committed")
	assert.Equal(t, "denied", logged[1]["outcome"])
	assert.NotContains(t, eventExtra(t, logged[1]), "mutation_committed")
}

func TestNewCedarServer_AuditsDeniedConsumerRead(t *testing.T) {
	t.Parallel()
	server, events := newAuditedCedarServer(t, newFakeSources())

	rec := doRequest(server, http.MethodGet, "/registry/private/v0.1/servers", "", "")
	require.Equal(t, http.StatusForbidden, rec.Code)
	rec = doRequest(server, http.MethodGet, "/registry/public/v0.1/servers", "", "")
	require.Equal(t, http.StatusOK, rec.Code)

	logged := events()
	require.Len(t, logged, 2)
	for i, want := range []struct{ registry, outcome string }{{"private", "denied"}, {"public", "success"}} {
		assert.Equal(t, "server.list", logged[i]["type"])
		assert.Equal(t, want.outcome, logged[i]["outcome"])
		target := logged[i]["target"].(map[string]any)
		assert.Equal(t, want.registry, target["registry_name"])
	}
}
