package cedar

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	catalogmocks "github.com/stacklok/toolhive-registry-server/pkg/registry/catalog/mocks"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

const (
	identityHeader = "X-Test-Identity" // "anonymous", "missing", or a subject
	groupsHeader   = "X-Test-Groups"   // comma-separated; "!" makes the claim malformed

	perViewPolicy = `permit(principal, action == Registry::Action::"catalog.read", resource == Registry::View::"public");`
	groupPolicy   = `
permit(principal, action == Registry::Action::"catalog.read", resource)
when { principal.authenticated };
permit(
  principal,
  action in [Registry::Action::"sources.read", Registry::Action::"views.read"],
  resource == Registry::Instance::"catalog"
) when { principal has groups && principal.groups.contains("registry-admins") };
`
)

func TestNewAuthorizerRequiresEngineAndIdentity(t *testing.T) {
	t.Parallel()
	engine, err := Load("")
	require.NoError(t, err)
	_, err = NewAuthorizer(nil, Options{Identity: headerIdentity})
	require.Error(t, err)
	_, err = NewAuthorizer(&Engine{}, Options{Identity: headerIdentity})
	require.Error(t, err)
	_, err = NewAuthorizer(engine, Options{})
	require.Error(t, err)
	a, err := NewAuthorizer(engine, Options{Identity: headerIdentity})
	require.NoError(t, err)
	require.NotNil(t, a)
}

func TestRouteActionsCoverLibraryRoutes(t *testing.T) {
	t.Parallel()
	consumer, admin := httpapi.ConsumerRoutes(), httpapi.AdminRoutes()
	actions, err := routeActions(consumer, admin)
	require.NoError(t, err)
	require.Len(t, actions, len(consumer)+len(admin), "every library route must map to exactly one action")
	for _, route := range consumer {
		require.Equal(t, CatalogRead, actions[route], "%s %s", route.Method, route.Template)
	}
	want := map[string]Action{
		"GET /sources": SourcesRead, "GET /sources/{name}": SourcesRead, "GET /sources/{name}/entries": SourcesRead,
		"PUT /sources/{name}": SourcesWrite, "DELETE /sources/{name}": SourcesDelete,
		"GET /registries": ViewsRead, "GET /registries/{name}": ViewsRead, "GET /registries/{name}/entries": ViewsRead,
		"PUT /registries/{name}": ViewsWrite, "DELETE /registries/{name}": ViewsDelete,
		"POST /entries": EntriesPublish, "DELETE /entries/{type}/{name}/versions/{version}": EntriesDelete,
	}
	require.Len(t, admin, len(want))
	for _, route := range admin {
		require.Equal(t, want[route.Method+" "+route.Template], actions[route], "%s %s", route.Method, route.Template)
	}
}

func TestRouteActionsRejectIncompleteInventories(t *testing.T) {
	t.Parallel()
	consumer, admin := httpapi.ConsumerRoutes(), httpapi.AdminRoutes()
	noRegistry := consumer[0]
	noRegistry.RegistryParam = ""
	tests := []struct {
		name     string
		consumer []httpapi.RouteDescriptor
		admin    []httpapi.RouteDescriptor
	}{
		{name: "unknown admin route", consumer: consumer,
			admin: append(append([]httpapi.RouteDescriptor{}, admin...), httpapi.RouteDescriptor{Method: http.MethodGet, Template: "/jobs"})},
		{name: "admin route missing from library", consumer: consumer, admin: admin[1:]},
		{name: "duplicate admin route", consumer: consumer, admin: append(append([]httpapi.RouteDescriptor{}, admin...), admin[0])},
		{name: "duplicate consumer route", consumer: append(append([]httpapi.RouteDescriptor{}, consumer...), consumer[0]), admin: admin},
		{name: "consumer route without registry parameter", consumer: []httpapi.RouteDescriptor{noRegistry}, admin: admin},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := routeActions(tc.consumer, tc.admin)
			require.Error(t, err)
		})
	}
}

func TestHTTPAdmission(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		policy     string
		mappings   []Mapping
		method     string
		path       string
		identity   string
		groups     string
		wantStatus int
		wantReach  bool
		// wantDecision is nil when OnDecision must not be called.
		wantDecision *Decision
		wantErr      error
	}{
		{
			name: "default policy admits authenticated consumer read", policy: DefaultPolicy,
			method: http.MethodGet, path: "/registry/main/v0.1/servers", identity: "alice",
			wantStatus: http.StatusOK, wantReach: true,
			wantDecision: &Decision{Action: CatalogRead, Resource: `Registry::View::"main"`, Allowed: true},
		},
		{
			name: "default policy admits anonymous consumer read", policy: DefaultPolicy,
			method: http.MethodGet, path: "/registry/main/v0.1/servers", identity: "anonymous",
			wantStatus: http.StatusOK, wantReach: true,
			wantDecision: &Decision{Principal: AnonymousPrincipal, Action: CatalogRead, Resource: `Registry::View::"main"`, Allowed: true},
		},
		{
			name: "default policy admits authenticated admin", policy: DefaultPolicy,
			method: http.MethodGet, path: "/v1/sources", identity: "alice",
			wantStatus: http.StatusOK, wantReach: true,
			wantDecision: &Decision{Action: SourcesRead, Resource: `Registry::Instance::"catalog"`, Allowed: true},
		},
		{
			name: "anonymous on admin route is 401", policy: DefaultPolicy,
			method: http.MethodGet, path: "/v1/sources", identity: "anonymous",
			wantStatus:   http.StatusUnauthorized,
			wantDecision: &Decision{Principal: AnonymousPrincipal, Action: SourcesRead, Resource: `Registry::Instance::"catalog"`},
			wantErr:      ErrAuthenticationRequired,
		},
		{
			name: "anonymous publish is 401", policy: DefaultPolicy,
			method: http.MethodPost, path: "/v1/entries", identity: "anonymous",
			wantStatus:   http.StatusUnauthorized,
			wantDecision: &Decision{Principal: AnonymousPrincipal, Action: EntriesPublish, Resource: `Registry::Instance::"catalog"`},
			wantErr:      ErrAuthenticationRequired,
		},
		{
			name: "missing identity is 500", policy: DefaultPolicy,
			method: http.MethodGet, path: "/registry/main/v0.1/servers", identity: "missing",
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "per-view policy admits public view", policy: perViewPolicy,
			method: http.MethodGet, path: "/registry/public/v0.1/servers", identity: "anonymous",
			wantStatus: http.StatusOK, wantReach: true,
			wantDecision: &Decision{Principal: AnonymousPrincipal, Action: CatalogRead, Resource: `Registry::View::"public"`, Allowed: true},
		},
		{
			name: "per-view policy denies other view", policy: perViewPolicy,
			method: http.MethodGet, path: "/registry/private/v0.1/servers", identity: "alice",
			wantStatus:   http.StatusForbidden,
			wantDecision: &Decision{Action: CatalogRead, Resource: `Registry::View::"private"`},
		},
		{
			name: "per-view policy admits skills slash alias", policy: perViewPolicy,
			method: http.MethodGet, path: "/registry/public/v0.1/x/dev.toolhive/skills/", identity: "anonymous",
			wantStatus: http.StatusOK, wantReach: true,
			wantDecision: &Decision{Principal: AnonymousPrincipal, Action: CatalogRead, Resource: `Registry::View::"public"`, Allowed: true},
		},
		{
			name: "per-view policy denies plugin detail on other view", policy: perViewPolicy,
			method: http.MethodGet, path: "/registry/private/v0.1/x/dev.toolhive/plugins/acme/tool", identity: "anonymous",
			wantStatus:   http.StatusForbidden,
			wantDecision: &Decision{Principal: AnonymousPrincipal, Action: CatalogRead, Resource: `Registry::View::"private"`},
		},
		{
			name: "percent-encoded view is decoded once", policy: `permit(principal, action, resource == Registry::View::"a/b");`,
			method: http.MethodGet, path: "/registry/a%2Fb/v0.1/servers", identity: "alice",
			wantStatus: http.StatusOK, wantReach: true,
			wantDecision: &Decision{Action: CatalogRead, Resource: `Registry::View::"a/b"`, Allowed: true},
		},
		{
			name: "whitespace view name is 400", policy: DefaultPolicy,
			method: http.MethodGet, path: "/registry/a%20b/v0.1/servers", identity: "alice",
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "per-view policy denies admin", policy: perViewPolicy,
			method: http.MethodGet, path: "/v1/sources", identity: "alice",
			wantStatus:   http.StatusForbidden,
			wantDecision: &Decision{Action: SourcesRead, Resource: `Registry::Instance::"catalog"`},
		},
		{
			name: "group policy admits admin group", policy: groupPolicy, mappings: testGroupMappings,
			method: http.MethodGet, path: "/v1/sources", identity: "alice", groups: "registry-admins",
			wantStatus: http.StatusOK, wantReach: true,
			wantDecision: &Decision{Action: SourcesRead, Resource: `Registry::Instance::"catalog"`, Allowed: true},
		},
		{
			name: "group policy denies caller outside group", policy: groupPolicy, mappings: testGroupMappings,
			method: http.MethodGet, path: "/v1/registries", identity: "bob", groups: "engineering",
			wantStatus:   http.StatusForbidden,
			wantDecision: &Decision{Action: ViewsRead, Resource: `Registry::Instance::"catalog"`},
		},
		{
			name: "group policy denies caller without groups claim", policy: groupPolicy, mappings: testGroupMappings,
			method: http.MethodGet, path: "/v1/sources", identity: "bob",
			wantStatus:   http.StatusForbidden,
			wantDecision: &Decision{Action: SourcesRead, Resource: `Registry::Instance::"catalog"`},
		},
		{
			name: "group policy denies write the group lacks", policy: groupPolicy, mappings: testGroupMappings,
			method: http.MethodDelete, path: "/v1/sources/git", identity: "alice", groups: "registry-admins",
			wantStatus:   http.StatusForbidden,
			wantDecision: &Decision{Action: SourcesDelete, Resource: `Registry::Instance::"catalog"`},
		},
		{
			name: "group policy denies anonymous consumer read", policy: groupPolicy, mappings: testGroupMappings,
			method: http.MethodGet, path: "/registry/main/v0.1/servers", identity: "anonymous",
			wantStatus:   http.StatusForbidden,
			wantDecision: &Decision{Principal: AnonymousPrincipal, Action: CatalogRead, Resource: `Registry::View::"main"`},
		},
		{
			name: "malformed mapped claim is 401", policy: groupPolicy, mappings: testGroupMappings,
			method: http.MethodGet, path: "/v1/sources", identity: "alice", groups: "!",
			wantStatus:   http.StatusUnauthorized,
			wantDecision: &Decision{Action: SourcesRead, Resource: `Registry::Instance::"catalog"`},
			wantErr:      ErrInvalidIdentity,
		},
		{
			name: "evaluation failure is 503", policy: overflowPolicy,
			method: http.MethodGet, path: "/registry/main/v0.1/servers", identity: "alice",
			wantStatus:   http.StatusServiceUnavailable,
			wantDecision: &Decision{Action: CatalogRead, Resource: `Registry::View::"main"`},
			wantErr:      ErrEvaluation,
		},
		{
			name: "evaluation failure on admin is 503", policy: overflowPolicy,
			method: http.MethodPost, path: "/v1/entries", identity: "alice",
			wantStatus:   http.StatusServiceUnavailable,
			wantDecision: &Decision{Action: EntriesPublish, Resource: `Registry::Instance::"catalog"`},
			wantErr:      ErrEvaluation,
		},
		{
			name: "protected host route admits anonymous metadata read", policy: DefaultPolicy,
			method: http.MethodGet, path: "/openapi.json", identity: "anonymous",
			wantStatus: http.StatusOK, wantReach: true,
			wantDecision: &Decision{Principal: AnonymousPrincipal, Action: MetadataRead, Resource: `Registry::Instance::"catalog"`, Allowed: true},
		},
		{
			name: "protected host route denied by per-view policy", policy: perViewPolicy,
			method: http.MethodGet, path: "/openapi.json", identity: "alice",
			wantStatus:   http.StatusForbidden,
			wantDecision: &Decision{Action: MetadataRead, Resource: `Registry::Instance::"catalog"`},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			engine, err := Parse([]byte(tc.policy))
			require.NoError(t, err)
			srv := newTestServer(t, engine, tc.mappings)

			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set(identityHeader, tc.identity)
			if tc.groups != "" {
				req.Header.Set(groupsHeader, tc.groups)
			}
			resp := httptest.NewRecorder()
			srv.handler.ServeHTTP(resp, req)

			require.Equal(t, tc.wantStatus, resp.Code, resp.Body.String())
			require.Equal(t, tc.wantReach, srv.reached.Load() > 0, "application reached")
			switch tc.wantStatus {
			case http.StatusForbidden, http.StatusServiceUnavailable:
				require.JSONEq(t, `{"error":"authorization denied or unavailable"}`, resp.Body.String())
			case http.StatusUnauthorized:
				require.JSONEq(t, `{"error":"unauthorized"}`, resp.Body.String())
				require.Equal(t, "Bearer", resp.Header().Get("WWW-Authenticate"))
			}

			decisions := srv.decisions()
			if tc.wantDecision == nil {
				require.Empty(t, decisions)
				return
			}
			require.Len(t, decisions, 1)
			got := decisions[0]
			want := *tc.wantDecision
			if want.Principal == "" {
				want.Principal = PrincipalID(testIssuer, tc.identity)
			}
			want.Revision = engine.Revision()
			require.ErrorIs(t, got.Err, tc.wantErr)
			got.Err, want.Err = nil, nil
			require.Equal(t, want, got)
		})
	}
}

func TestDecorateFailsClosedOnWiringFaults(t *testing.T) {
	t.Parallel()
	engine, err := Load("")
	require.NoError(t, err)
	var decided atomic.Int32
	a, err := NewAuthorizer(engine, Options{Identity: headerIdentity,
		OnDecision: func(*http.Request, Decision) { decided.Add(1) }})
	require.NoError(t, err)
	unreachable := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("handler reached despite a wiring fault")
	})
	servers := httpapi.ConsumerServerRoutes()[0]

	tests := []struct {
		name     string
		route    httpapi.RouteDescriptor
		next     http.Handler
		register string // chi pattern the decorated handler is mounted at
		method   string
		path     string
	}{
		{name: "unknown descriptor", route: httpapi.RouteDescriptor{Method: http.MethodGet, Template: "/jobs"},
			next: unreachable, register: "/jobs", method: http.MethodGet, path: "/jobs"},
		{name: "descriptor with altered field", route: withAuditEvent(servers, "other"),
			next: unreachable, register: servers.Template, method: http.MethodGet, path: "/main/v0.1/servers"},
		{name: "mounted at another template", route: servers,
			next: unreachable, register: "/{registryName}/servers", method: http.MethodGet, path: "/main/servers"},
		{name: "method mismatch", route: servers,
			next: unreachable, register: servers.Template, method: http.MethodPost, path: "/main/v0.1/servers"},
		{name: "nil next handler", route: servers,
			next: nil, register: servers.Template, method: http.MethodGet, path: "/main/v0.1/servers"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			router := chi.NewRouter()
			router.Handle(tc.register, a.Decorate(tc.route, tc.next))
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set(identityHeader, "alice")
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, req)
			require.Equal(t, http.StatusInternalServerError, resp.Code)
		})
	}

	t.Run("no chi route context", func(t *testing.T) {
		t.Parallel()
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/main/v0.1/servers", nil)
		req.Header.Set(identityHeader, "alice")
		a.Decorate(servers, unreachable).ServeHTTP(resp, req)
		require.Equal(t, http.StatusInternalServerError, resp.Code)
	})

	t.Run("protect rejects catalog.read and unknown actions", func(t *testing.T) {
		t.Parallel()
		for _, action := range []Action{CatalogRead, "root", ""} {
			resp := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
			req.Header.Set(identityHeader, "alice")
			a.Protect(action, unreachable).ServeHTTP(resp, req)
			require.Equal(t, http.StatusInternalServerError, resp.Code, action)
		}
	})
	// Subtests run after the parent returns, so the decision count is checked in Cleanup.
	t.Cleanup(func() { require.Zero(t, decided.Load(), "wiring faults are not decisions") })
}

func TestChallengeOverride(t *testing.T) {
	t.Parallel()
	engine, err := Load("")
	require.NoError(t, err)
	const challenge = `Bearer resource_metadata="https://registry.example.org/.well-known/oauth-protected-resource"`
	a, err := NewAuthorizer(engine, Options{Identity: headerIdentity,
		Challenge: func(*http.Request) string { return challenge }})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	req.Header.Set(identityHeader, "anonymous")
	resp := httptest.NewRecorder()
	a.Protect(SourcesRead, http.NotFoundHandler()).ServeHTTP(resp, req)
	require.Equal(t, http.StatusUnauthorized, resp.Code)
	require.Equal(t, challenge, resp.Header().Get("WWW-Authenticate"))
}

func TestAuthorizerConcurrentUse(t *testing.T) {
	t.Parallel()
	engine, err := Parse([]byte(groupPolicy))
	require.NoError(t, err)
	srv := newTestServer(t, engine, testGroupMappings)
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/v1/sources", nil)
			req.Header.Set(identityHeader, "alice")
			want := http.StatusForbidden
			if i%2 == 0 {
				req.Header.Set(groupsHeader, "registry-admins")
				want = http.StatusOK
			}
			resp := httptest.NewRecorder()
			srv.handler.ServeHTTP(resp, req)
			if resp.Code != want {
				t.Errorf("request %d: status %d, want %d", i, resp.Code, want)
			}
		}()
	}
	wg.Wait()
	require.Len(t, srv.decisions(), 32)
}

var testGroupMappings = []Mapping{{Issuer: testIssuer, Groups: &AttributeMapping{Claim: "groups", Format: FormatStringArray}}}

// headerIdentity stands in for host authentication: it reads the caller from
// test headers instead of a verified token.
func headerIdentity(r *http.Request) (Identity, bool) {
	subject := r.Header.Get(identityHeader)
	switch subject {
	case "missing", "":
		return Identity{}, false
	case "anonymous":
		return Identity{Anonymous: true}, true
	}
	claims := map[string]any{"iss": testIssuer, "sub": subject}
	switch groups := r.Header.Get(groupsHeader); groups {
	case "":
	case "!":
		claims["groups"] = 42
	default:
		values := []any{}
		for _, g := range strings.Split(groups, ",") {
			values = append(values, g)
		}
		claims["groups"] = values
	}
	return Identity{Issuer: testIssuer, Subject: subject, Claims: claims}, true
}

type testServer struct {
	handler http.Handler
	reached atomic.Int32
	mu      sync.Mutex
	log     []Decision
}

func (s *testServer) decisions() []Decision {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Decision(nil), s.log...)
}

// newTestServer mounts the library consumer router at /registry, the admin
// router at /v1 and a protected /openapi.json, as a host would.
func newTestServer(t *testing.T, engine *Engine, mappings []Mapping) *testServer {
	t.Helper()
	srv := &testServer{}
	a, err := NewAuthorizer(engine, Options{
		Identity: headerIdentity,
		Mappings: mappings,
		OnDecision: func(_ *http.Request, d Decision) {
			srv.mu.Lock()
			defer srv.mu.Unlock()
			srv.log = append(srv.log, d)
		},
	})
	require.NoError(t, err)

	consumer, err := httpapi.ConsumerRouter(&stubPages{reached: &srv.reached}, httpapi.ConsumerOptions{Decorate: a.Decorate})
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	sources := catalogmocks.NewMockSources(ctrl)
	sources.EXPECT().ListSources(gomock.Any()).DoAndReturn(func(context.Context) ([]persistence.SourceDefinition, error) {
		srv.reached.Add(1)
		return nil, nil
	}).AnyTimes()
	views := catalogmocks.NewMockViews(ctrl)
	views.EXPECT().ListViews(gomock.Any()).DoAndReturn(func(context.Context) ([]persistence.ViewDefinition, error) {
		srv.reached.Add(1)
		return nil, nil
	}).AnyTimes()
	diagnostics, err := catalog.NewDiagnostics(catalogmocks.NewMockEntryDiagnostics(ctrl), catalogmocks.NewMockSourceStatusReader(ctrl))
	require.NoError(t, err)
	admin, err := httpapi.AdminRouter(httpapi.AdminOperations{
		Sources: sources, Views: views, Publisher: catalogmocks.NewMockManagedPublisher(ctrl), Diagnostics: diagnostics,
	}, httpapi.AdminOptions{Decorate: a.Decorate})
	require.NoError(t, err)

	root := chi.NewRouter()
	root.Mount("/registry", consumer)
	root.Mount("/v1", admin)
	root.Method(http.MethodGet, "/openapi.json", a.Protect(MetadataRead, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		srv.reached.Add(1)
		w.WriteHeader(http.StatusOK)
	})))
	srv.handler = root
	return srv
}

// stubPages is a ConsumerPages that records whether admission let a request through.
type stubPages struct{ reached *atomic.Int32 }

func (s *stubPages) List(context.Context, string, catalog.Query) (catalog.Page, error) {
	s.reached.Add(1)
	return catalog.Page{}, nil
}

func (s *stubPages) Get(context.Context, string, catalog.EntryName, catalog.VersionSelector) (persistence.Entry, error) {
	s.reached.Add(1)
	return persistence.Entry{}, persistence.ErrNotFound
}

func (s *stubPages) ListVersions(context.Context, string, catalog.EntryName, catalog.PageRequest) (catalog.Page, error) {
	s.reached.Add(1)
	return catalog.Page{}, nil
}

func withAuditEvent(route httpapi.RouteDescriptor, event string) httpapi.RouteDescriptor {
	route.AuditEvent = event
	return route
}
