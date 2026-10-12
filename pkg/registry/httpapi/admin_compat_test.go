package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

const validInline = `{"version":"1.0.0","meta":{"last_updated":"2025-01-15T10:30:00Z"},"data":{"servers":[{"name":"com.example/weather","description":"claims are ordinary data","version":"1.0","packages":[]}]}}`

func TestAdminLegacySourceRequestsAndMutationSchemas(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		request      service.SourceCreateRequest
		definition   persistence.SourceDefinition
		sourceConfig any
	}{
		{"git", service.SourceCreateRequest{Git: &config.GitConfig{Repository: "https://example.org", Branch: "main", Path: "registry.json", Auth: &config.GitAuthConfig{Username: "host", PasswordFile: "/not-read-by-http"}}, SyncPolicy: &config.SyncPolicyConfig{Interval: "1h"}, Filter: &config.FilterConfig{Tags: &config.TagFilterConfig{Exclude: []string{"private"}}}}, persistence.SourceDefinition{Name: "git", Git: &persistence.GitSpec{Repository: "https://example.org", Branch: "main", Path: "registry.json", Auth: &persistence.GitAuth{Username: "host", PasswordFile: "/not-read-by-http"}}, Schedule: "1h", Filter: &persistence.Filter{Tags: &persistence.NameFilter{Exclude: []string{"private"}}}}, &config.GitConfig{Repository: "https://example.org", Branch: "main", Path: "registry.json", Auth: &config.GitAuthConfig{Username: "host", PasswordFile: "/not-read-by-http"}}},
		{"api", service.SourceCreateRequest{API: &config.APIConfig{Endpoint: "https://example.org", Timeout: "10s"}, SyncPolicy: &config.SyncPolicyConfig{Interval: "1h"}}, persistence.SourceDefinition{Name: "api", API: &persistence.APISpec{Endpoint: "https://example.org", Timeout: "10s"}, Schedule: "1h"}, &config.APIConfig{Endpoint: "https://example.org", Timeout: "10s"}},
		{"file", service.SourceCreateRequest{File: &config.FileConfig{Data: validInline}}, persistence.SourceDefinition{Name: "file", File: &persistence.FileSpec{Data: validInline}}, &config.FileConfig{Data: validInline}},
		{"managed", service.SourceCreateRequest{Managed: &config.ManagedConfig{}}, persistence.SourceDefinition{Name: "managed", Managed: &persistence.ManagedSpec{}}, &config.ManagedConfig{}},
		{"kubernetes", service.SourceCreateRequest{Kubernetes: &config.KubernetesConfig{Namespaces: []string{"one", "two"}}}, persistence.SourceDefinition{Name: "kubernetes", Kubernetes: &persistence.KubernetesSpec{Namespaces: []string{"one", "two"}}}, &config.KubernetesConfig{Namespaces: []string{"one", "two"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newAdminFixture(t)
			at := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
			stored := tc.definition
			stored.ID, stored.Origin, stored.CreatedAt, stored.UpdatedAt = "assigned", persistence.OriginAPI, at, at
			f.sources.EXPECT().CreateSource(gomock.Any(), tc.definition).Return(stored, nil)
			body, err := json.Marshal(tc.request)
			if err != nil {
				t.Fatal(err)
			}
			want := service.SourceInfo{Name: tc.name, Type: tc.name, SourceType: config.SourceType(tc.name), CreationType: service.CreationTypeAPI, SourceConfig: tc.sourceConfig, FilterConfig: tc.request.Filter, SyncSchedule: tc.definition.Schedule, CreatedAt: at, UpdatedAt: at}
			h := mountAdmin(t, f.ops, httpapi.AdminOptions{})
			adminJSON(t, adminResponse(t, h, "PUT", "/v1/sources/"+tc.name, string(body)), 201, want)
			f.sources.EXPECT().ListSources(gomock.Any()).Return([]persistence.SourceDefinition{stored}, nil)
			f.status.EXPECT().Status(gomock.Any(), tc.name).Return(persistence.JobStatus{SourceID: stored.ID, Phase: persistence.JobCompleted}, nil)
			want.SyncStatus = &service.SourceSyncStatus{Phase: "complete"}
			adminJSON(t, adminResponse(t, h, "GET", "/v1/sources", ""), 200, service.SourceListResponse{Sources: []service.SourceInfo{want}})
		})
	}
}

func TestAdminViewMutationAndListSchemas(t *testing.T) {
	t.Parallel()
	f := newAdminFixture(t)
	input := persistence.ViewDefinition{Name: "public", Sources: []string{"one", "two"}}
	stored := input
	stored.Origin = persistence.OriginAPI
	stored.CreatedAt = time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	stored.UpdatedAt = stored.CreatedAt
	want := service.RegistryInfo{Name: stored.Name, CreationType: service.CreationTypeAPI, Sources: stored.Sources, CreatedAt: stored.CreatedAt, UpdatedAt: stored.UpdatedAt}
	h := mountAdmin(t, f.ops, httpapi.AdminOptions{})
	f.views.EXPECT().CreateView(gomock.Any(), input).Return(stored, nil)
	adminJSON(t, adminResponse(t, h, "PUT", "/v1/registries/public", `{"sources":["one","two"]}`), 201, want)
	f.views.EXPECT().CreateView(gomock.Any(), input).Return(persistence.ViewDefinition{}, persistence.ErrAlreadyExists)
	f.views.EXPECT().UpdateView(gomock.Any(), "public", input).Return(stored, nil)
	adminJSON(t, adminResponse(t, h, "PUT", "/v1/registries/public", `{"sources":["one","two"]}`), 200, want)
	f.views.EXPECT().ListViews(gomock.Any()).Return([]persistence.ViewDefinition{stored}, nil)
	adminJSON(t, adminResponse(t, h, "GET", "/v1/registries", ""), 200, struct {
		Registries []service.RegistryInfo `json:"registries"`
	}{[]service.RegistryInfo{want}})
}

func TestAdminNestedClaimsUnknownExtensionsAndDottedKeys(t *testing.T) {
	t.Parallel()
	f := newAdminFixture(t)
	f.sources.EXPECT().CreateSource(gomock.Any(), persistence.SourceDefinition{Name: "item", Managed: &persistence.ManagedSpec{}}).Return(persistence.SourceDefinition{Name: "item", Managed: &persistence.ManagedSpec{}}, nil)
	h := mountAdmin(t, f.ops, httpapi.AdminOptions{})
	if w := adminResponse(t, h, "PUT", "/v1/sources/item", `{"managed":{},"claims.team":null}`); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, kind := range []string{"server", "skill", "plugin"} {
		body := `{"` + kind + `":{"$schema":"https://example.org/server.schema.json","namespace":"com.example","name":"item","version":"1","description":"published","metadata":{"claims":{"nested":true}},"_meta":{"claims":{"nested":true}},"futureExtension":{"claims":null}}}`
		if kind == "server" {
			body = `{"server":{"$schema":"https://example.org/server.schema.json","name":"com.example/item","version":"1","description":"published","_meta":{"io.modelcontextprotocol.registry/publisher-provided":{"claims":{"nested":true}}},"futureExtension":{"claims":null}}}`
		}
		f.publisher.EXPECT().Publish(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e persistence.Entry) (persistence.Entry, error) { return e, nil })
		if w := adminResponse(t, h, "POST", "/v1/entries", body); w.Code != 201 || !strings.Contains(w.Body.String(), `"claims":{"nested":true}`) {
			t.Fatal(kind, w.Code, w.Body.String())
		}
	}
}

func TestAdminViewAndPublisherPartialCommit(t *testing.T) {
	t.Parallel()
	for _, resource := range []string{"registry", "entry"} {
		t.Run(resource, func(t *testing.T) {
			t.Parallel()
			f := newAdminFixture(t)
			failure := &catalog.PostCommitError{Cause: errors.Join(persistence.ErrAlreadyExists, context.Canceled, errors.New("private password"))}
			method, path, body := "PUT", "/v1/registries/public", `{"sources":["one"]}`
			event := "registry.create"
			if resource == "registry" {
				view := persistence.ViewDefinition{Name: "public", Sources: []string{"one"}}
				f.views.EXPECT().CreateView(gomock.Any(), view).Return(view, failure)
			} else {
				method, path, body, event = "POST", "/v1/entries", `{"skill":{"namespace":"com.example","name":"item","version":"1","description":"published"}}`, "entry.publish"
				f.publisher.EXPECT().Publish(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e persistence.Entry) (persistence.Entry, error) { return e, failure })
			}
			var mutation httpapi.MutationResult
			h := mountAdmin(t, f.ops, httpapi.AdminOptions{Decorate: func(_ httpapi.RouteDescriptor, next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					next.ServeHTTP(w, r.WithContext(httpapi.WithMutationObserver(r.Context(), func(result httpapi.MutationResult) { mutation = result })))
				})
			}})
			w := adminResponse(t, h, method, path, body)
			var wire map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &wire); err != nil {
				t.Fatal(err)
			}
			if w.Code != 503 || wire["committed"] != true || wire["resource"] == nil || strings.Contains(w.Body.String(), "private password") || !mutation.Committed || mutation.AuditEvent != event {
				t.Fatal(w.Code, w.Body.String(), mutation)
			}
		})
	}
}

func TestAdminStatusProjectionAllowlist(t *testing.T) {
	t.Parallel()
	for _, failure := range []persistence.FailureCategory{persistence.FailureFetch, persistence.FailureValidation, persistence.FailureStorage, persistence.FailureCancelled, "password=private"} {
		t.Run(string(failure), func(t *testing.T) {
			t.Parallel()
			f := newAdminFixture(t)
			f.sources.EXPECT().GetSource(gomock.Any(), "item").Return(persistence.SourceDefinition{Name: "item", ID: "private-incarnation", Managed: &persistence.ManagedSpec{}}, nil)
			f.status.EXPECT().Status(gomock.Any(), "item").Return(persistence.JobStatus{SourceID: "private-incarnation", Phase: persistence.JobFailed, Failure: failure, LastHash: "private-hash", LastFilterHash: "private-filter-hash", LeaseExpiresAt: new(time.Time)}, nil)
			w := adminResponse(t, mountAdmin(t, f.ops, httpapi.AdminOptions{}), "GET", "/v1/sources/item", "")
			if w.Code != 200 || strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "lease") || strings.Contains(w.Body.String(), "hash") {
				t.Fatal(w.Code, w.Body.String())
			}
			var wire struct {
				SyncStatus struct{ Phase, Message string }
			}
			if err := json.Unmarshal(w.Body.Bytes(), &wire); err != nil {
				t.Fatal(err)
			}
			message := ""
			if persistence.ValidateFailureCategory(failure) == nil {
				message = string(failure)
			}
			if wire.SyncStatus.Phase != "failed" || wire.SyncStatus.Message != message {
				t.Fatal(wire)
			}
		})
	}
}

func TestAdminInvalidCommittedPublicationResult(t *testing.T) {
	t.Parallel()
	f := newAdminFixture(t)
	f.publisher.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(persistence.Entry{Kind: persistence.SkillKind, Plugin: &model.Plugin{}}, nil)
	w := adminResponse(t, mountAdmin(t, f.ops, httpapi.AdminOptions{}), "POST", "/v1/entries", `{"skill":{"namespace":"com.example","name":"item","version":"1","description":"published"}}`)
	adminJSON(t, w, 503, map[string]any{"error": "mutation committed; follow-up failed", "committed": true})
}
