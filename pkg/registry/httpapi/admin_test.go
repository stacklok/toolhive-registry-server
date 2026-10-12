package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/mock/gomock"

	legacy "github.com/stacklok/toolhive-registry-server/internal/api/v1"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog/mocks"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

type adminFixture struct {
	sources   *mocks.MockSources
	views     *mocks.MockViews
	publisher *mocks.MockManagedPublisher
	entries   *mocks.MockEntryDiagnostics
	status    *mocks.MockSourceStatusReader
	ops       httpapi.AdminOperations
}

func newAdminFixture(t *testing.T) adminFixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := adminFixture{sources: mocks.NewMockSources(ctrl), views: mocks.NewMockViews(ctrl),
		publisher: mocks.NewMockManagedPublisher(ctrl), entries: mocks.NewMockEntryDiagnostics(ctrl), status: mocks.NewMockSourceStatusReader(ctrl)}
	diag, err := catalog.NewDiagnostics(f.entries, f.status)
	if err != nil {
		t.Fatal(err)
	}
	f.ops = httpapi.AdminOperations{Sources: f.sources, Views: f.views, Publisher: f.publisher, Diagnostics: diag}
	return f
}

func mountAdmin(t *testing.T, ops httpapi.AdminOperations, options httpapi.AdminOptions) http.Handler {
	t.Helper()
	h, err := httpapi.AdminRouter(ops, options)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Mount("/v1", h)
	return r
}

func adminResponse(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	return w
}

func adminJSON(t *testing.T, w *httptest.ResponseRecorder, code int, want any) {
	t.Helper()
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var gotJSON, wantJSON any
	if err := json.Unmarshal(w.Body.Bytes(), &gotJSON); err != nil {
		t.Fatal(w.Body.String(), err)
	}
	if err := json.Unmarshal(data, &wantJSON); err != nil {
		t.Fatal(err)
	}
	if w.Code != code || !reflect.DeepEqual(gotJSON, wantJSON) {
		t.Fatalf("got %d %s; want %d %s", w.Code, w.Body.String(), code, data)
	}
}

func TestAdminConstructionAndInventory(t *testing.T) {
	t.Parallel()
	f := newAdminFixture(t)
	for _, missing := range []string{"sources", "views", "publisher", "diagnostics"} {
		ops := f.ops
		switch missing {
		case "sources":
			ops.Sources = nil
		case "views":
			ops.Views = nil
		case "publisher":
			ops.Publisher = nil
		case "diagnostics":
			ops.Diagnostics = nil
		}
		if _, err := httpapi.AdminRouter(ops, httpapi.AdminOptions{}); !errors.Is(err, persistence.ErrInvalid) || !strings.Contains(err.Error(), missing) {
			t.Fatal(missing, err)
		}
	}
	for _, options := range []httpapi.AdminOptions{
		{Middleware: []func(http.Handler) http.Handler{nil}},
		{Decorate: func(httpapi.RouteDescriptor, http.Handler) http.Handler { return nil }},
	} {
		if _, err := httpapi.AdminRouter(f.ops, options); !errors.Is(err, persistence.ErrInvalid) {
			t.Fatal(err)
		}
	}
	inventory := httpapi.AdminRoutes()
	if len(inventory) != 12 {
		t.Fatal(inventory)
	}
	decorated := make(map[string]httpapi.RouteDescriptor)
	h, err := httpapi.AdminRouter(f.ops, httpapi.AdminOptions{Decorate: func(d httpapi.RouteDescriptor, next http.Handler) http.Handler {
		decorated[d.Method+" "+d.Template] = d
		return next
	}})
	if err != nil {
		t.Fatal(err)
	}
	actual, old := make(map[string]bool), make(map[string]bool)
	walk := func(r http.Handler, out map[string]bool) {
		t.Helper()
		if err := chi.Walk(r.(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			if route != "/me" && !strings.HasSuffix(route, "/claims") {
				out[method+" "+route] = true
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	walk(h, actual)
	walk(legacy.Router(nil, nil), old)
	if !reflect.DeepEqual(actual, old) || len(actual) != len(decorated) {
		t.Fatal(actual, old, decorated)
	}
	for _, d := range inventory {
		if decorated[d.Method+" "+d.Template] != d || d.ResourceType == "" {
			t.Fatal(d)
		}
		if d.Operation == httpapi.Upsert {
			if d.AuditCreateEvent != d.ResourceType+".create" || d.AuditUpdateEvent != d.ResourceType+".update" {
				t.Fatal(d)
			}
		} else if d.AuditEvent == "" {
			t.Fatal(d)
		}
	}
	inventory[0].Template = "changed"
	if httpapi.AdminRoutes()[0].Template == "changed" {
		t.Fatal("inventory aliases internal data")
	}
	mux := mountAdmin(t, f.ops, httpapi.AdminOptions{})
	for _, path := range []string{"/v1/me", "/v1/entries/server/item/claims", "/v1/sources/", "/health", "/readiness", "/version", "/metrics", "/registry/public/v0.1/servers"} {
		if w := adminResponse(t, mux, "GET", path, ""); w.Code != 404 {
			t.Fatal(path, w.Code)
		}
	}
}

//nolint:exhaustive // Each closed route family excludes consumer-only operations.
func TestAdminEveryRouteDispatchAndHostWrappers(t *testing.T) {
	t.Parallel()
	for _, d := range httpapi.AdminRoutes() {
		t.Run(d.Method+d.Template, func(t *testing.T) {
			t.Parallel()
			f := newAdminFixture(t)
			source := persistence.SourceDefinition{Name: "item", ID: "incarnation", Managed: &persistence.ManagedSpec{}, Origin: persistence.OriginAPI}
			view := persistence.ViewDefinition{Name: "item", Sources: []string{"item"}, Origin: persistence.OriginAPI}
			body, code := "", 200
			switch d.ResourceType {
			case "source":
				switch d.Operation {
				case httpapi.List:
					f.sources.EXPECT().ListSources(gomock.Any()).Return([]persistence.SourceDefinition{source}, nil)
					f.status.EXPECT().Status(gomock.Any(), "item").Return(persistence.JobStatus{SourceID: source.ID}, nil)
				case httpapi.Get:
					f.sources.EXPECT().GetSource(gomock.Any(), "item").Return(source, nil)
					f.status.EXPECT().Status(gomock.Any(), "item").Return(persistence.JobStatus{SourceID: source.ID}, nil)
				case httpapi.Upsert:
					body, code = `{"managed":{}}`, 201
					f.sources.EXPECT().CreateSource(gomock.Any(), persistence.SourceDefinition{Name: "item", Managed: &persistence.ManagedSpec{}}).Return(source, nil)
				case httpapi.Delete:
					code = 204
					f.sources.EXPECT().DeleteSource(gomock.Any(), "item").Return(nil)
				case httpapi.ListEntries:
					f.entries.EXPECT().ListSourceVersions(gomock.Any(), "item").Return([]persistence.DiagnosticVersion{}, nil)
				}
			case "registry":
				switch d.Operation {
				case httpapi.List:
					f.views.EXPECT().ListViews(gomock.Any()).Return([]persistence.ViewDefinition{view}, nil)
				case httpapi.Get:
					f.views.EXPECT().GetView(gomock.Any(), "item").Return(view, nil)
				case httpapi.Upsert:
					body, code = `{"sources":["item"]}`, 201
					f.views.EXPECT().CreateView(gomock.Any(), persistence.ViewDefinition{Name: "item", Sources: []string{"item"}}).Return(view, nil)
				case httpapi.Delete:
					code = 204
					f.views.EXPECT().DeleteView(gomock.Any(), "item").Return(nil)
				case httpapi.ListEntries:
					f.entries.EXPECT().ListViewVersions(gomock.Any(), "item").Return([]persistence.ViewDiagnosticVersion{}, nil)
				}
			case "entry":
				if d.Operation == httpapi.Publish {
					body, code = `{"server":{"$schema":"https://example.org/server.schema.json","name":"com.example/item","version":"1","description":"published"}}`, 201
					f.publisher.EXPECT().Publish(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e persistence.Entry) (persistence.Entry, error) { return e, nil })
				} else {
					code = 204
					f.publisher.EXPECT().DeleteManaged(gomock.Any(), persistence.ServerKind, "com.example/item", "latest").Return(nil)
				}
			}
			decorations, calls := 0, 0
			var mutation httpapi.MutationResult
			h := mountAdmin(t, f.ops, httpapi.AdminOptions{
				Middleware: []func(http.Handler) http.Handler{func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Host-Middleware", "yes")
						next.ServeHTTP(w, r)
					})
				}},
				Decorate: func(info httpapi.RouteDescriptor, next http.Handler) http.Handler {
					decorations++
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls++
						if info != d {
							t.Error(info, d)
						}
						next.ServeHTTP(w, r.WithContext(httpapi.WithMutationObserver(r.Context(), func(result httpapi.MutationResult) { mutation = result })))
					})
				},
			})
			path := "/v1" + strings.NewReplacer("{name}", "item", "{type}", "server", "{version}", "latest").Replace(d.Template)
			if d.ResourceType == "entry" && d.Operation == httpapi.Delete {
				path = "/v1/entries/server/com.example%2Fitem/versions/latest"
			}
			w := adminResponse(t, h, d.Method, path, body)
			if w.Code != code || decorations != 12 || calls != 1 || w.Header().Get("Host-Middleware") != "yes" {
				t.Fatal(w.Code, w.Body.String(), decorations, calls)
			}
			if code == 201 || code == 204 {
				if !mutation.Committed || mutation.AuditEvent == "" {
					t.Fatal(mutation)
				}
			} else if mutation.Committed {
				t.Fatal(mutation)
			}
		})
	}
}

func TestAdminHostAdmissionIsResponsible(t *testing.T) {
	t.Parallel()
	f := newAdminFixture(t)
	h := mountAdmin(t, f.ops, httpapi.AdminOptions{Decorate: func(_ httpapi.RouteDescriptor, _ http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) })
	}})
	if w := adminResponse(t, h, "PUT", "/v1/sources/item", `{"managed":{}}`); w.Code != 403 {
		t.Fatal(w.Code)
	}
}

func TestAdminWireMatchesLegacyWithoutClaims(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, source := range []persistence.SourceDefinition{
		{Name: "git", Git: &persistence.GitSpec{Repository: "https://example.org", Auth: &persistence.GitAuth{Username: "user", PasswordFile: "/secret/ref"}}, Schedule: "1h", Filter: &persistence.Filter{Names: &persistence.NameFilter{Include: []string{"a"}}}},
		{Name: "api", API: &persistence.APISpec{Endpoint: "https://example.org", Timeout: "10s"}, Schedule: "1h"},
		{Name: "file", File: &persistence.FileSpec{Path: "/registry.json"}, Schedule: "1h"},
		{Name: "managed", Managed: &persistence.ManagedSpec{}},
		{Name: "kubernetes", Kubernetes: &persistence.KubernetesSpec{Namespaces: []string{"default"}}},
	} {
		t.Run(source.Name, func(t *testing.T) {
			t.Parallel()
			f := newAdminFixture(t)
			source.ID, source.Origin, source.CreatedAt, source.UpdatedAt = "incarnation", persistence.OriginConfig, at, at
			f.sources.EXPECT().GetSource(gomock.Any(), source.Name).Return(source, nil)
			f.status.EXPECT().Status(gomock.Any(), source.Name).Return(persistence.JobStatus{SourceID: source.ID, Phase: persistence.JobCompleted, StartedAt: &at, EndedAt: &at, Attempts: 2, ServerCount: 3, SkillCount: 4, PluginCount: 5, LastHash: "secret hash", LeaseExpiresAt: &at}, nil)
			var cfg any
			switch source.Name {
			case "git":
				cfg = &config.GitConfig{Repository: source.Git.Repository, Auth: &config.GitAuthConfig{Username: "user", PasswordFile: "/secret/ref"}}
			case "api":
				cfg = &config.APIConfig{Endpoint: source.API.Endpoint, Timeout: "10s"}
			case "file":
				cfg = &config.FileConfig{Path: "/registry.json"}
			case "managed":
				cfg = &config.ManagedConfig{}
			case "kubernetes":
				cfg = &config.KubernetesConfig{Namespaces: []string{"default"}}
			}
			want := service.SourceInfo{Name: source.Name, Type: source.Name, SourceType: config.SourceType(source.Name), CreationType: service.CreationTypeCONFIG, SourceConfig: cfg, SyncSchedule: source.Schedule, CreatedAt: at, UpdatedAt: at, SyncStatus: &service.SourceSyncStatus{Phase: "complete", LastSyncTime: &at, LastAttempt: &at, AttemptCount: 2, ServerCount: 3, SkillCount: 4, PluginCount: 5}}
			if source.Filter != nil {
				want.FilterConfig = &config.FilterConfig{Names: source.Filter.Names}
			}
			adminJSON(t, adminResponse(t, mountAdmin(t, f.ops, httpapi.AdminOptions{}), "GET", "/v1/sources/"+source.Name, ""), 200, want)
		})
	}
	f := newAdminFixture(t)
	v := persistence.ViewDefinition{Name: "public", Origin: persistence.OriginAPI, Sources: []string{"one", "two"}, CreatedAt: at, UpdatedAt: at}
	f.views.EXPECT().GetView(gomock.Any(), "public").Return(v, nil)
	adminJSON(t, adminResponse(t, mountAdmin(t, f.ops, httpapi.AdminOptions{}), "GET", "/v1/registries/public", ""), 200, service.RegistryInfo{Name: v.Name, CreationType: service.CreationTypeAPI, Sources: v.Sources, CreatedAt: at, UpdatedAt: at})
}

func TestAdminDiagnosticWireKeepsAllVersionsAndSources(t *testing.T) {
	t.Parallel()
	f := newAdminFixture(t)
	at := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	rows := []persistence.DiagnosticVersion{
		{Kind: persistence.ServerKind, Name: "com.example/item", Version: "1", Title: "title", Description: "description", CreatedAt: at, UpdatedAt: at},
		{Kind: persistence.ServerKind, Name: "com.example/item", Version: "2", CreatedAt: at, UpdatedAt: at},
		{Kind: persistence.SkillKind, Name: "item", Version: "1", CreatedAt: at, UpdatedAt: at},
		{Kind: persistence.PluginKind, Name: "item", Version: "1", CreatedAt: at, UpdatedAt: at},
	}
	f.entries.EXPECT().ListSourceVersions(gomock.Any(), "one").Return(rows, nil)
	versions := func(row persistence.DiagnosticVersion) service.EntryVersionInfo {
		return service.EntryVersionInfo{Version: row.Version, Title: row.Title, Description: row.Description, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
	}
	h := mountAdmin(t, f.ops, httpapi.AdminOptions{})
	adminJSON(t, adminResponse(t, h, "GET", "/v1/sources/one/entries", ""), 200, service.SourceEntriesResponse{Entries: []service.SourceEntryInfo{
		{EntryType: "MCP", Name: rows[0].Name, Versions: []service.EntryVersionInfo{versions(rows[0]), versions(rows[1])}},
		{EntryType: "SKILL", Name: "item", Versions: []service.EntryVersionInfo{versions(rows[2])}},
		{EntryType: "PLUGIN", Name: "item", Versions: []service.EntryVersionInfo{versions(rows[3])}},
	}})
	var viewRows []persistence.ViewDiagnosticVersion
	var want []service.RegistryEntryInfo
	for _, row := range rows {
		for i, source := range []string{"one", "two"} {
			viewRows = append(viewRows, persistence.ViewDiagnosticVersion{DiagnosticVersion: row, SourceName: source, SourceID: "private incarnation", Position: int32(i)})
			want = append(want, service.RegistryEntryInfo{EntryType: string(row.Kind), Name: row.Name, Version: row.Version, Title: row.Title, Description: row.Description, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, SourceName: source, Position: int32(i)})
		}
	}
	f.entries.EXPECT().ListViewVersions(gomock.Any(), "public").Return(viewRows, nil)
	adminJSON(t, adminResponse(t, h, "GET", "/v1/registries/public/entries", ""), 200, service.RegistryEntriesResponse{Entries: want})
}

func TestAdminPUTOnlyAlreadyExistsUpdates(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		err    error
		code   int
		update bool
	}{
		{"create", nil, 201, false}, {"update", persistence.ErrAlreadyExists, 200, true},
		{"config", persistence.ErrConfigOwned, 403, false}, {"singleton", persistence.ErrConflict, 409, false},
		{"kind", persistence.ErrKindChange, 400, false}, {"inuse", persistence.ErrInUse, 409, false},
		{"invalid", persistence.ErrInvalid, 400, false}, {"absent", persistence.ErrNotFound, 404, false},
		{"unavailable", fmt.Errorf("private backend: %w", persistence.ErrUnavailable), 500, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newAdminFixture(t)
			input := persistence.SourceDefinition{Name: "item", Managed: &persistence.ManagedSpec{}}
			f.sources.EXPECT().CreateSource(gomock.Any(), input).Return(input, tc.err)
			if tc.update {
				f.sources.EXPECT().UpdateSource(gomock.Any(), "item", input).Return(input, nil)
			}
			w := adminResponse(t, mountAdmin(t, f.ops, httpapi.AdminOptions{}), "PUT", "/v1/sources/item", `{"managed":{},"origin":"CONFIG","id":"user-picked","creationType":"CONFIG","createdAt":"ignored","updatedAt":"ignored"}`)
			if w.Code != tc.code || strings.Contains(w.Body.String(), "private backend") {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}

func TestAdminViewPUTAndReferencedSourceErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err    error
		code   int
		update bool
	}{
		{nil, 201, false}, {persistence.ErrAlreadyExists, 200, true}, {persistence.ErrConfigOwned, 403, false},
		{persistence.ErrReferenceNotFound, 400, false}, {persistence.ErrNotFound, 404, false}, {persistence.ErrConflict, 409, false},
	} {
		t.Run(fmt.Sprint(tc.code), func(t *testing.T) {
			t.Parallel()
			f := newAdminFixture(t)
			v := persistence.ViewDefinition{Name: "public", Sources: []string{"one"}}
			f.views.EXPECT().CreateView(gomock.Any(), v).Return(v, tc.err)
			if tc.update {
				f.views.EXPECT().UpdateView(gomock.Any(), "public", v).Return(v, nil)
			}
			w := adminResponse(t, mountAdmin(t, f.ops, httpapi.AdminOptions{}), "PUT", "/v1/registries/public", `{"sources":["one"],"origin":"CONFIG","createdAt":"ignored"}`)
			if w.Code != tc.code {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}

//nolint:exhaustive // This table deliberately exercises only the three definition mutations.
func TestAdminPostCommitPrecedesUnwrappedErrorAndPreservesAudit(t *testing.T) {
	t.Parallel()
	for _, operation := range []httpapi.Operation{httpapi.Create, httpapi.Update, httpapi.Delete} {
		for _, cause := range []error{persistence.ErrAlreadyExists, context.Canceled, context.DeadlineExceeded} {
			t.Run(string(operation)+cause.Error(), func(t *testing.T) {
				t.Parallel()
				f := newAdminFixture(t)
				input := persistence.SourceDefinition{Name: "item", Managed: &persistence.ManagedSpec{}}
				stored := input
				stored.Origin = persistence.OriginAPI
				err := &catalog.PostCommitError{Cause: errors.Join(errors.New("private callback password"), cause)}
				method, body := "PUT", `{"managed":{}}`
				switch operation {
				case httpapi.Create:
					f.sources.EXPECT().CreateSource(gomock.Any(), input).Return(stored, err)
				case httpapi.Update:
					f.sources.EXPECT().CreateSource(gomock.Any(), input).Return(persistence.SourceDefinition{}, persistence.ErrAlreadyExists)
					f.sources.EXPECT().UpdateSource(gomock.Any(), "item", input).Return(stored, err)
				case httpapi.Delete:
					method, body = "DELETE", ""
					f.sources.EXPECT().DeleteSource(gomock.Any(), "item").Return(err)
				}
				var results []httpapi.MutationResult
				h := mountAdmin(t, f.ops, httpapi.AdminOptions{Decorate: func(_ httpapi.RouteDescriptor, next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						next.ServeHTTP(w, r.WithContext(httpapi.WithMutationObserver(r.Context(), func(result httpapi.MutationResult) { results = append(results, result) })))
					})
				}})
				w := adminResponse(t, h, method, "/v1/sources/item", body)
				var wire map[string]any
				if err := json.Unmarshal(w.Body.Bytes(), &wire); err != nil {
					t.Fatal(err)
				}
				if w.Code != 503 || wire["committed"] != true || strings.Contains(w.Body.String(), "private callback") || len(results) != 1 || results[0] != (httpapi.MutationResult{Operation: operation, Committed: true, AuditEvent: "source." + string(operation)}) {
					t.Fatal(w.Code, w.Body.String(), results)
				}
				if operation != httpapi.Delete && wire["resource"] == nil {
					t.Fatal("stored resource missing")
				}
			})
		}
	}
}

func TestAdminStatusFailuresAndIncarnationMismatch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status persistence.JobStatus
		err    error
		code   int
	}{
		{"unavailable", persistence.JobStatus{}, fmt.Errorf("private status failure: %w", persistence.ErrUnavailable), 500},
		{"recreated", persistence.JobStatus{SourceID: "other"}, nil, 500},
		{"deleted", persistence.JobStatus{}, persistence.ErrNotFound, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newAdminFixture(t)
			f.sources.EXPECT().GetSource(gomock.Any(), "item").Return(persistence.SourceDefinition{Name: "item", ID: "expected", Managed: &persistence.ManagedSpec{}}, nil)
			f.status.EXPECT().Status(gomock.Any(), "item").Return(tc.status, tc.err)
			w := adminResponse(t, mountAdmin(t, f.ops, httpapi.AdminOptions{}), "GET", "/v1/sources/item", "")
			if w.Code != tc.code || strings.Contains(w.Body.String(), "private status") {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}

func publicationInput(kind persistence.EntryKind) persistence.Entry {
	e := persistence.Entry{Kind: kind, Name: "item", Version: "1"}
	switch kind {
	case persistence.ServerKind:
		e.Name = "com.example/item"
		e.Server = &model.Server{Schema: "https://example.org/server.schema.json", Name: e.Name, Version: e.Version, Description: "published"}
	case persistence.SkillKind:
		e.Skill = &model.Skill{Namespace: "com.example", Name: e.Name, Version: e.Version, Description: "published", Metadata: map[string]any{"claims": nil}}
	case persistence.PluginKind:
		e.Plugin = &model.Plugin{Namespace: "com.example", Name: e.Name, Version: e.Version, Description: "published", Meta: map[string]any{"claims": map[string]any{"nested": true}}}
	}
	return e
}

func TestAdminPublishAllKindsAndExactDelete(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind persistence.EntryKind
		wire string
	}{{persistence.ServerKind, "server"}, {persistence.SkillKind, "skill"}, {persistence.PluginKind, "plugin"}} {
		t.Run(tc.wire, func(t *testing.T) {
			t.Parallel()
			f := newAdminFixture(t)
			input := publicationInput(tc.kind)
			var payload any
			switch tc.kind {
			case persistence.ServerKind:
				payload = input.Server
			case persistence.SkillKind:
				payload = input.Skill
			case persistence.PluginKind:
				payload = input.Plugin
			}
			body, err := json.Marshal(map[string]any{tc.wire: payload})
			if err != nil {
				t.Fatal(err)
			}
			f.publisher.EXPECT().Publish(gomock.Any(), input).Return(input, nil)
			h := mountAdmin(t, f.ops, httpapi.AdminOptions{})
			adminJSON(t, adminResponse(t, h, "POST", "/v1/entries", string(body)), 201, payload)
			f.publisher.EXPECT().DeleteManaged(gomock.Any(), tc.kind, input.Name, "latest").Return(nil)
			name := input.Name
			if tc.kind == persistence.ServerKind {
				name = "com.example%2Fitem"
			}
			if w := adminResponse(t, h, "DELETE", "/v1/entries/"+tc.wire+"/"+name+"/versions/latest", ""); w.Code != 204 {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}

func TestAdminPublicationErrorMapping(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err  error
		code int
	}{
		{persistence.ErrNoManagedSource, 500}, {persistence.ErrNotFound, 404}, {persistence.ErrConflict, 409},
		{persistence.ErrInvalid, 400}, {persistence.ErrUnavailable, 500}, {context.Canceled, 408}, {context.DeadlineExceeded, 504},
	} {
		t.Run(tc.err.Error(), func(t *testing.T) {
			t.Parallel()
			f := newAdminFixture(t)
			f.publisher.EXPECT().DeleteManaged(gomock.Any(), persistence.SkillKind, "item", "1").Return(fmt.Errorf("private: %w", tc.err))
			w := adminResponse(t, mountAdmin(t, f.ops, httpapi.AdminOptions{}), "DELETE", "/v1/entries/skill/item/versions/1", "")
			if w.Code != tc.code || strings.Contains(w.Body.String(), "private") {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}
