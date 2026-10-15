package authorization

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/cedar-policy/cedar-go/types"
	"github.com/go-chi/chi/v5"

	"github.com/stacklok/toolhive-registry-server/internal/audit"
	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

type evidenceKey struct{}
type evidence struct {
	principal types.Entity
	action    Action
	route     string
	method    string
	request   *http.Request
	response  *admissionResponse
	admitted  bool
	resource  types.EntityUID
	operation httpapi.Operation
	kind      persistence.EntryKind
}

// Registrar is mandatory for standalone route construction. Only it creates route evidence.
type Registrar struct {
	engine      *Engine
	mappings    []config.IdentityMapping
	routes      map[string]bool
	auditLogger *audit.Logger
}

// NewRegistrar requires a preflighted evaluator.
func NewRegistrar(engine *Engine, mappings []config.IdentityMapping, auditLogger *audit.Logger) (*Registrar, error) {
	if engine == nil || engine.policies == nil {
		return nil, ErrEvaluation
	}
	return &Registrar{engine: engine, mappings: mappings, routes: map[string]bool{}, auditLogger: auditLogger}, nil
}

// Register binds an exact method/template to its closed action and existing audited handler.
func (g *Registrar) Register(router chi.Router, method, template string, action Action,
	handler http.Handler, authenticate func(http.Handler) http.Handler) error {
	if router == nil || handler == nil || authenticate == nil || !known(action) || method == "" || template == "" {
		return ErrEvidence
	}
	key := method + " " + template
	if g.routes[key] {
		return ErrEvidence
	}
	g.routes[key] = true
	router.Method(method, template, authenticate(g.wrap(method, template, action, httpapi.RouteDescriptor{}, handler)))
	return nil
}

func (g *Registrar) wrap(
	method, template string, action Action, descriptor httpapi.RouteDescriptor, handler http.Handler,
) http.Handler {
	expected := strings.TrimSuffix(template, "/") // Chi's RoutePattern removes trailing slashes.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method || chi.RouteContext(r.Context()).RoutePattern() != expected {
			http.Error(w, "authorization wiring failure", 500)
			return
		}
		identity, ok := auth.VerifiedIdentityFromContext(r.Context())
		if !ok {
			http.Error(w, "authorization identity missing", 500)
			return
		}
		if identity.Anonymous && action != CatalogRead && action != MetadataRead {
			auth.WriteAdmissionUnauthorized(w, r, "authentication required")
			return
		}
		p, err := principal(identity, g.mappings)
		if err != nil {
			auth.WriteAdmissionUnauthorized(w, r, "invalid authentication attributes")
			return
		}
		response := &admissionResponse{ResponseWriter: w}
		ev := &evidence{principal: p, action: action, route: expected, method: method, request: r, response: response,
			resource: instance, operation: descriptor.Operation, kind: descriptor.Kind}
		if action == CatalogRead {
			if descriptor.RegistryParam == "" {
				http.Error(w, "authorization wiring failure", http.StatusInternalServerError)
				return
			}
			view, err := requestedView(r, descriptor.RegistryParam)
			if err != nil {
				http.Error(w, "invalid registry name", http.StatusBadRequest)
				return
			}
			ev.resource = types.EntityUID{Type: viewType, ID: types.String(view)}
		}
		if ev.operation == httpapi.GetLatest {
			ev.operation = httpapi.Get
		}
		ctx := context.WithValue(r.Context(), evidenceKey{}, ev)
		if action == MetadataRead {
			if err := g.admit(ctx, MetadataRead); err != nil {
				return
			}
		}
		handler.ServeHTTP(response, r.WithContext(ctx))
	})
}

func requestedView(r *http.Request, param string) (string, error) {
	// Match the public HTTP adapter's decoding and validation; never normalize names.
	view, err := url.PathUnescape(chi.URLParam(r, param))
	if err != nil || strings.TrimSpace(view) == "" || strings.ContainsAny(view, " \t\n\r") ||
		len(view) > 4096 || !utf8.ValidString(view) || strings.ContainsRune(view, 0) {
		return "", persistence.ErrInvalid
	}
	return view, nil
}

func (g *Registrar) admit(ctx context.Context, action Action) error {
	return g.admitResource(ctx, action, instance, "", "")
}

func (g *Registrar) admitCatalog(
	ctx context.Context, view string, operation httpapi.Operation, kind persistence.EntryKind,
) error {
	return g.admitResource(ctx, CatalogRead, types.EntityUID{Type: viewType, ID: types.String(view)}, operation, kind)
}

func (g *Registrar) admitResource(ctx context.Context, action Action, resource types.EntityUID,
	operation httpapi.Operation, kind persistence.EntryKind) error {
	ev, ok := ctx.Value(evidenceKey{}).(*evidence)
	if !ok || ev == nil || ev.response.rejected {
		return ErrEvidence
	}
	if !ev.matches(ctx, action, resource, operation, kind) {
		g.logDecision(ctx, ev, action, "error")
		http.Error(ev.response.ResponseWriter, "authorization wiring failure", http.StatusInternalServerError)
		ev.response.rejected = true
		return ErrEvidence
	}
	if ev.admitted {
		return nil
	}
	err := g.engine.evaluate(ev.principal, action, resource)
	decision := "deny"
	if err == nil {
		decision = "allow"
	}
	if errors.Is(err, ErrEvaluation) || errors.Is(err, ErrEvidence) {
		decision = "error"
	}
	g.logDecision(ctx, ev, action, decision)
	if err != nil {
		status := http.StatusForbidden
		if errors.Is(err, ErrEvaluation) {
			status = http.StatusServiceUnavailable
		}
		if errors.Is(err, ErrEvidence) {
			status = http.StatusInternalServerError
		}
		http.Error(ev.response.ResponseWriter, "authorization denied or unavailable", status)
		ev.response.rejected = true
		return err
	}
	ev.admitted = true
	return nil
}

func (ev *evidence) matches(ctx context.Context, action Action, resource types.EntityUID,
	operation httpapi.Operation, kind persistence.EntryKind) bool {
	return ev.action == action && ev.method == ev.request.Method && ev.resource == resource &&
		(action != CatalogRead || ev.operation == operation && ev.kind == kind) &&
		ev.route == chi.RouteContext(ctx).RoutePattern()
}

func (g *Registrar) logDecision(ctx context.Context, ev *evidence, action Action, decision string) {
	if g.auditLogger != nil {
		g.auditLogger.Admission(ctx, ev.request, string(ev.principal.UID.ID), string(action),
			ev.resource.String(), g.engine.revision, decision)
		return
	}
	slog.InfoContext(ctx, "Cedar admission", "event_type", "authorization.admission",
		"actor", string(ev.principal.UID.ID), "action", string(action),
		"decision", decision, "resource", ev.resource.String(), "policy_revision", g.engine.revision)
}

type admissionResponse struct {
	http.ResponseWriter
	rejected bool
}

func (w *admissionResponse) WriteHeader(code int) {
	if !w.rejected {
		w.ResponseWriter.WriteHeader(code)
	}
}
func (w *admissionResponse) Write(data []byte) (int, error) {
	if w.rejected {
		return len(data), nil
	}
	return w.ResponseWriter.Write(data)
}
