// Package httpapi provides consumer and optional admin HTTP adapters over host-selected capabilities.
// Hosts own admission, middleware, listeners and lifecycle; construction performs no IO.
package httpapi

import (
	"context"
	"net/http"
	"reflect"

	"github.com/go-chi/chi/v5"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// ConsumerPages is structurally satisfied by catalog.Reader and by replacement
// applications. Each operation selects a coherent bounded page; HTTP never
// filters, sorts, refills, deduplicates or queries another backend.
type ConsumerPages interface {
	List(context.Context, string, catalog.Query) (catalog.Page, error)
	Get(context.Context, string, catalog.EntryName, catalog.VersionSelector) (persistence.Entry, error)
	ListVersions(context.Context, string, catalog.EntryName, catalog.PageRequest) (catalog.Page, error)
}

// Operation describes domain intent, not an authorization action or identity.
type Operation string

// Consumer domain operations distinguish list, history, unversioned extension
// reads, and versioned reads. GetLatest classifies only the unversioned skill and
// plugin route family; a versioned /versions/latest route remains Get and selects
// the latest version from its path parameter.
const (
	List         Operation = "list"
	Get          Operation = "get"
	GetLatest    Operation = "get-latest"
	ListVersions Operation = "list-versions"
)

// RouteDescriptor describes one actual registration. Template is relative to the
// host's /registry (consumer) or /v1 (admin) mount. ResourceParam is the legacy audit name parameter;
// RegistryParam and NamespaceParam let hosts resolve scoped resource metadata.
type RouteDescriptor struct {
	Method         string
	Template       string
	Kind           persistence.EntryKind
	Operation      Operation
	AuditEvent     string
	ResourceType   string
	ResourceParam  string
	RegistryParam  string
	NamespaceParam string
	// AuditCreateEvent/AuditUpdateEvent discriminate an admin PUT. Hosts must
	// observe MutationResult, not infer committed mutations solely from status.
	AuditCreateEvent string
	AuditUpdateEvent string
	// KindParam identifies the server/skill/plugin discriminator on admin deletion.
	KindParam string
}

// ConsumerOptions supplies ordinary host middleware and optional per-route
// decoration. Decorate runs once per registration, including slash aliases.
// A host may leave routes unprotected, or require the complete manifest for admission.
type ConsumerOptions struct {
	Middleware []func(http.Handler) http.Handler
	Decorate   func(RouteDescriptor, http.Handler) http.Handler
}

const (
	registryNameParam  = "registryName"
	serverNameParam    = "serverName"
	serverResourceType = "server"
)

// ConsumerRoutes returns a fresh inventory of every registered method/template:
// eleven canonical routes and two extension list trailing-slash aliases.
func ConsumerRoutes() []RouteDescriptor {
	prefix := "/{registryName}/v0.1"
	routes := []RouteDescriptor{
		{Method: http.MethodGet, Template: prefix + "/servers", Kind: persistence.ServerKind,
			Operation: List, AuditEvent: "server.list", ResourceType: serverResourceType,
			ResourceParam: serverNameParam, RegistryParam: registryNameParam},
		{Method: http.MethodGet, Template: prefix + "/servers/{serverName}/versions", Kind: persistence.ServerKind,
			Operation: ListVersions, AuditEvent: "server.versions.list", ResourceType: serverResourceType,
			ResourceParam: serverNameParam, RegistryParam: registryNameParam},
		{Method: http.MethodGet, Template: prefix + "/servers/{serverName}/versions/{version}", Kind: persistence.ServerKind,
			Operation: Get, AuditEvent: "server.version.read", ResourceType: serverResourceType,
			ResourceParam: serverNameParam, RegistryParam: registryNameParam},
	}
	for _, family := range []struct {
		path, resource string
		kind           persistence.EntryKind
	}{
		{"skills", "skill", persistence.SkillKind}, {"plugins", "plugin", persistence.PluginKind},
	} {
		for _, route := range []struct {
			suffix    string
			operation Operation
			event     string
		}{
			{"", List, listAuditSuffix}, {"/", List, listAuditSuffix},
			{"/{namespace}/{name}", GetLatest, ".read"},
			{"/{namespace}/{name}/versions", ListVersions, ".versions.list"},
			{"/{namespace}/{name}/versions/{version}", Get, ".version.read"},
		} {
			routes = append(routes, RouteDescriptor{Method: http.MethodGet,
				Template: prefix + "/x/dev.toolhive/" + family.path + route.suffix,
				Kind:     family.kind, Operation: route.operation, AuditEvent: family.resource + route.event,
				ResourceType: family.resource, ResourceParam: nameParam, RegistryParam: registryNameParam, NamespaceParam: "namespace"})
		}
	}
	return routes
}

// ConsumerRouter returns an unstarted, policy-free handler to mount under /registry.
// Omitting Decorate explicitly leaves admission to the host; use Authorize for a
// protected boundary (nil authorizers deny). It mounts
// no admin or operational routes and extracts no authentication/claims data.
// Dependencies must be initialized; nil (including typed nil) is rejected.
func ConsumerRouter(pages ConsumerPages, options ConsumerOptions) (http.Handler, error) {
	if isNil(pages) {
		return nil, persistence.ErrInvalid
	}
	r := chi.NewRouter()
	for _, mw := range options.Middleware {
		if mw == nil {
			return nil, persistence.ErrInvalid
		}
		r.Use(mw)
	}
	for _, route := range ConsumerRoutes() {
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			serveConsumer(pages, route, w, req)
		})
		if options.Decorate != nil {
			handler = options.Decorate(route, handler)
			if isNil(handler) {
				return nil, persistence.ErrInvalid
			}
		}
		r.Method(route.Method, route.Template, handler)
	}
	return r, nil
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	kind := v.Kind()
	if kind == reflect.Chan || kind == reflect.Func || kind == reflect.Interface ||
		kind == reflect.Map || kind == reflect.Pointer || kind == reflect.Slice {
		return v.IsNil()
	}
	return false
}
