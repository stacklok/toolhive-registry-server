package host

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// Four independent application replacements; none exposes the old monolith,
// authorization configuration, storage creation or a listener.
type sourceApplication struct{ calls int }

func (a *sourceApplication) CreateSource(_ context.Context, s persistence.SourceDefinition) (persistence.SourceDefinition, error) {
	a.calls++
	s.ID = "host incarnation"
	s.Origin = persistence.OriginAPI
	return s, nil
}
func (*sourceApplication) GetSource(context.Context, string) (persistence.SourceDefinition, error) {
	return persistence.SourceDefinition{}, persistence.ErrNotFound
}
func (*sourceApplication) ListSources(context.Context) ([]persistence.SourceDefinition, error) {
	return []persistence.SourceDefinition{}, nil
}
func (*sourceApplication) UpdateSource(context.Context, string, persistence.SourceDefinition) (persistence.SourceDefinition, error) {
	return persistence.SourceDefinition{}, persistence.ErrUnavailable
}
func (*sourceApplication) DeleteSource(context.Context, string) error {
	return persistence.ErrUnavailable
}

type viewApplication struct{ calls int }

func (a *viewApplication) CreateView(_ context.Context, v persistence.ViewDefinition) (persistence.ViewDefinition, error) {
	a.calls++
	v.Origin = persistence.OriginAPI
	return v, nil
}
func (*viewApplication) GetView(context.Context, string) (persistence.ViewDefinition, error) {
	return persistence.ViewDefinition{}, persistence.ErrNotFound
}
func (*viewApplication) ListViews(context.Context) ([]persistence.ViewDefinition, error) {
	return []persistence.ViewDefinition{}, nil
}
func (*viewApplication) UpdateView(context.Context, string, persistence.ViewDefinition) (persistence.ViewDefinition, error) {
	return persistence.ViewDefinition{}, persistence.ErrUnavailable
}
func (*viewApplication) DeleteView(context.Context, string) error { return persistence.ErrUnavailable }

type publicationApplication struct{ calls int }

func (a *publicationApplication) Publish(_ context.Context, e persistence.Entry) (persistence.Entry, error) {
	a.calls++
	return e, nil
}
func (a *publicationApplication) DeleteManaged(context.Context, persistence.EntryKind, string, string) error {
	a.calls++
	return nil
}

type diagnosticApplication struct{ calls int }

func (a *diagnosticApplication) ListSourceVersions(context.Context, string) ([]persistence.DiagnosticVersion, error) {
	a.calls++
	return []persistence.DiagnosticVersion{{Kind: persistence.SkillKind, Name: "host supplied", Version: "1"}}, nil
}
func (*diagnosticApplication) ListViewVersions(context.Context, string) ([]persistence.ViewDiagnosticVersion, error) {
	return []persistence.ViewDiagnosticVersion{}, nil
}
func (*diagnosticApplication) Status(context.Context, string) (persistence.JobStatus, error) {
	return persistence.JobStatus{}, persistence.ErrUnavailable
}

var (
	_ httpapi.SourceAdmin      = catalog.DefinitionAdmin(nil)
	_ httpapi.ViewAdmin        = catalog.DefinitionAdmin(nil)
	_ httpapi.EntryPublisher   = catalog.Publisher(nil)
	_ httpapi.AdminDiagnostics = catalog.Diagnostics(nil)
)

func TestHostMountsIndependentAdminApplications(t *testing.T) {
	sources, views, publisher, diagnostics := &sourceApplication{}, &viewApplication{}, &publicationApplication{}, &diagnosticApplication{}
	operations := httpapi.AdminOperations{Sources: sources, Views: views, Publisher: publisher, Diagnostics: diagnostics}
	var mutations []httpapi.MutationResult
	decorations := 0
	h, err := httpapi.AdminRouter(operations, httpapi.AdminOptions{
		Middleware: []func(http.Handler) http.Handler{func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Host-Middleware", "yes")
				next.ServeHTTP(w, r)
			})
		}},
		Decorate: func(d httpapi.RouteDescriptor, next http.Handler) http.Handler {
			decorations++
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Host-Operation", string(d.Operation))
				next.ServeHTTP(w, r.WithContext(httpapi.WithMutationObserver(r.Context(), func(result httpapi.MutationResult) { mutations = append(mutations, result) })))
			})
		},
	})
	if err != nil || decorations != 12 || sources.calls+views.calls+publisher.calls+diagnostics.calls != 0 {
		t.Fatal(err, decorations)
	}
	mux := chi.NewRouter()
	mux.Mount("/v1", h)
	for _, request := range []struct {
		method, path, body string
		code               int
	}{
		{"PUT", "/v1/sources/host", `{"managed":{}}`, 201},
		{"PUT", "/v1/registries/host", `{"sources":["host"]}`, 201},
		{"POST", "/v1/entries", `{"skill":{"namespace":"com.example","name":"item","version":"1","description":"host"}}`, 201},
		{"GET", "/v1/sources/host/entries", "", 200},
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(request.method, request.path, strings.NewReader(request.body)))
		if w.Code != request.code || w.Header().Get("Host-Middleware") != "yes" || w.Header().Get("Host-Operation") == "" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if sources.calls != 1 || views.calls != 1 || publisher.calls != 1 || diagnostics.calls != 1 || len(mutations) != 3 || mutations[0].AuditEvent != "source.create" || mutations[1].AuditEvent != "registry.create" || mutations[2].AuditEvent != "entry.publish" {
		t.Fatal("hidden backend call or incorrect route evidence", mutations)
	}
}
