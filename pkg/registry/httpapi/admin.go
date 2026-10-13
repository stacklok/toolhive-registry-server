package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// SourceAdmin is the source application capability consumed by AdminRouter.
// catalog.DefinitionAdmin satisfies it; replacements need no view operations.
type SourceAdmin interface{ persistence.Sources }

// ViewAdmin manages named ordered views (called registries on the wire).
type ViewAdmin interface{ persistence.Views }

// EntryPublisher publishes/removes exact versions in the singleton managed source.
type EntryPublisher interface{ persistence.ManagedPublisher }

// AdminDiagnostics supplies complete raw projections and safe job status.
// It is deliberately not ConsumerPages, job mutation or readiness.
type AdminDiagnostics interface {
	persistence.EntryDiagnostics
	persistence.SourceStatusReader
}

// AdminOperations contains independently replaceable initialized capabilities.
// There is no fallback backend; typed nil implementations are outside the caller contract.
type AdminOperations struct {
	Sources     SourceAdmin
	Views       ViewAdmin
	Publisher   EntryPublisher
	Diagnostics AdminDiagnostics
}

// AdminOptions reuses the consumer host middleware and per-route decorator contract.
type AdminOptions = ConsumerOptions

const (
	nameParam         = "name"
	nameSuffix        = "/{name}"
	listAuditSuffix   = ".list"
	entryResourceType = "entry"
)

// Admin domain operation strings; they are not authorization actions.
const (
	Upsert      = "upsert"
	Create      = "create"
	Update      = "update"
	Delete      = "delete"
	Publish     = "publish"
	ListEntries = "list-entries"
)

// MutationResult identifies a persisted mutation even if follow-up work failed.
// HTTP failure is not proof of rollback. This contains no identity or payload.
type MutationResult struct {
	Operation  Operation
	AuditEvent string
	Committed  bool
}

type mutationObserverKey struct{}

// WithMutationObserver lets host decorators observe the actual create/update
// outcome, including partial commits. The callback is synchronous, at most once
// per request, and only called for a known committed mutation, before writing HTTP.
// It must not panic or retry the mutation. Hosts own all auditing and attribution.
func WithMutationObserver(ctx context.Context, observe func(MutationResult)) context.Context {
	return context.WithValue(ctx, mutationObserverKey{}, observe)
}

// AdminRoutes returns the twelve retained registrations, relative to /v1.
// There are no slash aliases, identity/claims tombstones or operational routes.
func AdminRoutes() []RouteDescriptor {
	routes := make([]RouteDescriptor, 0, 12)
	for _, family := range []struct{ path, resource string }{
		{"sources", "source"}, {"registries", "registry"},
	} {
		prefix := "/" + family.path
		for _, route := range []struct {
			method, suffix string
			operation      Operation
			event          string
		}{
			{http.MethodGet, "", List, listAuditSuffix},
			{http.MethodGet, nameSuffix, Get, ".read"},
			{http.MethodPut, nameSuffix, Upsert, ""},
			{http.MethodDelete, nameSuffix, Delete, ".delete"},
			{http.MethodGet, "/{name}/entries", ListEntries, ".entries.list"},
		} {
			d := RouteDescriptor{Method: route.method, Template: prefix + route.suffix,
				Operation: route.operation, ResourceType: family.resource}
			if route.suffix != "" {
				d.ResourceParam = nameParam
			}
			if route.operation == Upsert {
				d.AuditCreateEvent, d.AuditUpdateEvent = family.resource+".create", family.resource+".update"
			} else {
				d.AuditEvent = family.resource + route.event
			}
			routes = append(routes, d)
		}
	}
	return append(routes,
		RouteDescriptor{Method: http.MethodPost, Template: "/entries", Operation: Publish,
			AuditEvent: "entry.publish", ResourceType: entryResourceType},
		RouteDescriptor{Method: http.MethodDelete, Template: "/entries/{type}/{name}/versions/{version}",
			Operation: Delete, AuditEvent: "entry.delete", ResourceType: entryResourceType, ResourceParam: nameParam, KindParam: "type"},
	)
}

// AdminRouter constructs an unstarted policy-free handler to mount explicitly at
// /v1. Omitting Decorate explicitly leaves admission to the host; Authorize with
// a nil authorizer denies. It performs no IO and owns no resources. Hosts must supply admission and
// audit wrappers for standalone use; consumer mounting never mounts this router.
// Nil errors name the missing capability. Dependencies must be initialized;
// typed nil dependencies and handlers are outside the caller contract (no reflection).
func AdminRouter(operations AdminOperations, options AdminOptions) (http.Handler, error) {
	for _, dependency := range []struct {
		name    string
		missing bool
	}{
		{"sources", operations.Sources == nil}, {"views", operations.Views == nil},
		{"publisher", operations.Publisher == nil}, {"diagnostics", operations.Diagnostics == nil},
	} {
		if dependency.missing {
			return nil, fmt.Errorf("%w: admin %s capability required", persistence.ErrInvalid, dependency.name)
		}
	}
	r := chi.NewRouter()
	for _, mw := range options.Middleware {
		if mw == nil {
			return nil, fmt.Errorf("%w: admin middleware required", persistence.ErrInvalid)
		}
		r.Use(mw)
	}
	for _, route := range AdminRoutes() {
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			serveAdmin(operations, route, w, req)
		})
		if options.Decorate != nil {
			handler = options.Decorate(route, handler)
			if handler == nil {
				return nil, fmt.Errorf("%w: admin decorator returned nil for %s %s", persistence.ErrInvalid, route.Method, route.Template)
			}
		}
		r.Method(route.Method, route.Template, handler)
	}
	return r, nil
}

func observeMutation(r *http.Request, route RouteDescriptor, operation Operation) {
	event := route.AuditEvent
	if operation == Create {
		event = route.AuditCreateEvent
	}
	if operation == Update {
		event = route.AuditUpdateEvent
	}
	if observe, ok := r.Context().Value(mutationObserverKey{}).(func(MutationResult)); ok && observe != nil {
		observe(MutationResult{Operation: operation, AuditEvent: event, Committed: true})
	}
}

func postCommitted(err error) bool {
	var committed *catalog.PostCommitError
	return errors.As(err, &committed)
}
