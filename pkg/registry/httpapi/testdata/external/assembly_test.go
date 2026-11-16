package host

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
)

// MountAdmin is an executable host-owned admission/audit assembly example.
// authenticate must verify identity and populate host context before admission.
// This is NOT the standalone server's Cedar policy implementation.
func MountAdmin(
	ops httpapi.AdminOperations,
	authenticate func(http.Handler) http.Handler,
	admit func(context.Context, httpapi.RouteDescriptor) bool,
	audit func(context.Context, httpapi.RouteDescriptor, httpapi.MutationResult, int),
) (http.Handler, error) {
	if authenticate == nil || admit == nil || audit == nil {
		return nil, errors.New("host authentication, admission and audit wrappers required")
	}
	admin, err := httpapi.AdminRouter(ops, httpapi.AdminOptions{
		Middleware: []func(http.Handler) http.Handler{authenticate},
		Decorate: func(d httpapi.RouteDescriptor, next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rw := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
				var mutation httpapi.MutationResult
				defer func() { audit(r.Context(), d, mutation, rw.Status()) }()
				if !admit(r.Context(), d) {
					http.Error(rw, "forbidden", http.StatusForbidden)
					return
				}
				ctx := httpapi.WithMutationObserver(r.Context(), func(result httpapi.MutationResult) { mutation = result })
				next.ServeHTTP(rw, r.WithContext(ctx))
			})
		},
	})
	if err != nil {
		return nil, err
	}
	mux := chi.NewRouter()
	mux.Mount("/v1", admin) // Consumer routing is a separate, explicit mount.
	return mux, nil
}

func TestHostAdmissionAndAuditAssembly(t *testing.T) {
	ops := httpapi.AdminOperations{Sources: &sourceApplication{}, Views: &viewApplication{}, Publisher: &publicationApplication{}, Diagnostics: &diagnosticApplication{}}
	if _, err := MountAdmin(ops, nil, nil, nil); err == nil {
		t.Fatal("missing host boundary accepted")
	}
	for _, allow := range []bool{false, true} {
		var audited httpapi.MutationResult
		var status int
		var descriptor httpapi.RouteDescriptor
		h, err := MountAdmin(ops, func(next http.Handler) http.Handler { return next }, func(_ context.Context, d httpapi.RouteDescriptor) bool {
			if d.Method != "PUT" || d.Operation != httpapi.Upsert || d.AuditCreateEvent != "source.create" || d.AuditUpdateEvent != "source.update" {
				t.Fatal(d)
			}
			return allow
		}, func(_ context.Context, d httpapi.RouteDescriptor, m httpapi.MutationResult, s int) {
			descriptor, audited, status = d, m, s
		})
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("PUT", "/v1/sources/example", strings.NewReader(`{"managed":{}}`)))
		want := 403
		if allow {
			want = 201
		}
		if w.Code != want || status != want || audited.Committed != allow || descriptor.Template != "/sources/{name}" {
			t.Fatal(w.Code, status, audited, descriptor)
		}
	}
}
