package host

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
)

type identityKey struct{}

// authenticateHost is a real host-owned credential check, not a trusted actor
// header. These credentials are test fixtures; deployments supply their own
// verifier and use TLS (as the external server test does).
func authenticateHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(user), []byte("fixture-user")) != 1 ||
			subtle.ConstantTimeCompare([]byte(password), []byte("fixture-password")) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="host"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, user)))
	})
}

// hostAuthorizer is non-Cedar admission over a host identity and domain intent.
// Hosts can instead call their own authorization service here.
type hostAuthorizer struct {
	allow bool
	err   error
	calls int
}

var _ httpapi.Authorizer = (*hostAuthorizer)(nil)

func (a *hostAuthorizer) Authorize(ctx context.Context, d httpapi.RouteDescriptor) (bool, error) {
	a.calls++
	if ctx.Value(identityKey{}) != "fixture-user" {
		return false, nil
	}
	switch d.Operation {
	case httpapi.List, httpapi.Upsert:
		return a.allow, a.err
	default:
		return false, nil
	}
}

// MountProtectedCatalog uses only the existing Middleware/Decorate seams.
// Consumer and admin capabilities stay policy-free; admission runs before them.
func MountProtectedCatalog(pages httpapi.ConsumerPages, ops httpapi.AdminOperations, authorizer httpapi.Authorizer) (http.Handler, error) {
	options := httpapi.ConsumerOptions{
		Middleware: []func(http.Handler) http.Handler{authenticateHost},
		Decorate:   httpapi.Authorize(authorizer),
	}
	consumer, err := httpapi.ConsumerRouter(pages, options)
	if err != nil {
		return nil, err
	}
	admin, err := httpapi.AdminRouter(ops, options)
	if err != nil {
		return nil, err
	}
	mux := chi.NewRouter()
	mux.Mount("/registry", consumer)
	mux.Mount("/v1", admin)
	return mux, nil
}

func TestProtectedHostAuthenticationAndNonCedarAdmission(t *testing.T) {
	for _, tc := range []struct {
		name       string
		allow      bool
		err        error
		credential string
		status     int
	}{
		{"allow", true, nil, "fixture-password", 200},
		{"deny", false, nil, "fixture-password", 403},
		{"allow with error", true, errors.New("private backend diagnostic"), "fixture-password", 403},
		{"invalid credential", true, nil, "wrong", 401},
		{"missing credential", true, nil, "", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pages, sources := &application{}, &sourceApplication{}
			authorizer := &hostAuthorizer{allow: tc.allow, err: tc.err}
			ops := httpapi.AdminOperations{Sources: sources, Views: &viewApplication{}, Publisher: &publicationApplication{}, Diagnostics: &diagnosticApplication{}}
			h, err := MountProtectedCatalog(pages, ops, authorizer)
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewTLSServer(h)
			defer server.Close()
			for _, request := range []struct{ method, path, body string }{
				{"GET", "/registry/public/v0.1/servers", ""},
				{"PUT", "/v1/sources/example", `{"managed":{}}`},
			} {
				req, err := http.NewRequest(request.method, server.URL+request.path, strings.NewReader(request.body))
				if err != nil {
					t.Fatal(err)
				}
				if tc.credential != "" {
					req.SetBasicAuth("fixture-user", tc.credential)
				}
				response, err := server.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, readErr := io.ReadAll(response.Body)
				response.Body.Close()
				if readErr != nil {
					t.Fatal(readErr)
				}
				if strings.Contains(string(body), "private backend diagnostic") {
					t.Fatal("authorizer diagnostics leaked")
				}
				want := tc.status
				if want == 200 && request.method == "PUT" {
					want = 201
				}
				if response.StatusCode != want {
					t.Fatalf("%s: got %d, want %d", request.path, response.StatusCode, want)
				}
			}
			wantCalls := 0
			if tc.status == 200 {
				wantCalls = 1
			}
			if pages.calls != wantCalls || sources.calls != wantCalls {
				t.Fatal("admission did not precede application", pages.calls, sources.calls)
			}
			if tc.status == 401 && authorizer.calls != 0 {
				t.Fatal("authorization preceded authentication")
			}
		})
	}
}

func TestProtectedHelperFailsClosedBeforeMutation(t *testing.T) {
	known := httpapi.AdminRoutes()[2] // PUT /sources/{name}
	for _, name := range []string{"nil", "typed nil", "unknown", "mismatched template", "mismatched method", "missing route context"} {
		t.Run(name, func(t *testing.T) {
			a := &hostAuthorizer{allow: true}
			var authorizer httpapi.Authorizer = a
			d := known
			method, template := known.Method, known.Template
			switch name {
			case "nil":
				authorizer = nil
			case "typed nil":
				authorizer = (*hostAuthorizer)(nil)
			case "unknown":
				d.Operation = "unknown"
			case "mismatched template":
				template = "/alternate/{name}"
			case "mismatched method":
				method = "POST"
			}
			mutations := 0
			h := httpapi.Authorize(authorizer)(d, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { mutations++ }))
			mux := chi.NewRouter()
			mux.Method(method, template, h)
			var target http.Handler = mux
			if name == "missing route context" {
				target = h
			}
			path := strings.ReplaceAll(template, "{name}", "example")
			w := httptest.NewRecorder()
			target.ServeHTTP(w, httptest.NewRequest(method, path, nil))
			if w.Code != 403 || mutations != 0 || a.calls != 0 {
				t.Fatal(w.Code, mutations, a.calls)
			}
		})
	}
}
