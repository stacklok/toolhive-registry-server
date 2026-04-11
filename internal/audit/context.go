package audit

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// RouteInfo carries pre-resolved audit metadata for a single route.
type RouteInfo struct {
	// EventType is the resolved event type for non-upsert routes.
	EventType string
	// OnCreate / OnUpdate are set for PUT upsert routes.
	// The middleware resolves between them using the observed HTTP status:
	// 201 → OnCreate, anything else → OnUpdate.
	OnCreate string
	OnUpdate string
	// Target is the pre-populated target map, built at request time from
	// chi URL params. It always contains "method" and "path"; optionally
	// "resource_type", "resource_name", "entry_type", "version",
	// "registry_name", and "namespace".
	Target map[string]string
}

// routeInfoCarrier is a mutable holder allocated by Middleware and populated
// by Audited* wrappers during handler execution.
type routeInfoCarrier struct {
	info *RouteInfo
}

type routeInfoKey struct{}

// newRouteInfoCarrier allocates a carrier and returns a context that holds it.
func newRouteInfoCarrier(ctx context.Context) context.Context {
	carrier := &routeInfoCarrier{}
	return context.WithValue(ctx, routeInfoKey{}, carrier)
}

// RouteInfoFromContext returns the RouteInfo injected by one of the Audited*
// wrappers, or nil if none was injected.
func RouteInfoFromContext(ctx context.Context) *RouteInfo {
	carrier, _ := ctx.Value(routeInfoKey{}).(*routeInfoCarrier)
	if carrier == nil {
		return nil
	}
	return carrier.info
}

// setRouteInfo writes info into the carrier held by ctx.
// It is a no-op when no carrier is present (e.g., in tests that bypass Middleware).
func setRouteInfo(ctx context.Context, info *RouteInfo) {
	if carrier, ok := ctx.Value(routeInfoKey{}).(*routeInfoCarrier); ok {
		carrier.info = info
	}
}

// Audited wraps a handler for routes with a single, unambiguous event type.
// resourceType is the ResourceType* constant (empty string for collection
// endpoints with no named resource). nameParam is the chi URL parameter name
// to use as "resource_name" (empty string when there is none).
func Audited(eventType, resourceType, nameParam string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := map[string]string{
			"method": r.Method,
			"path":   r.URL.Path,
		}
		if resourceType != "" {
			target["resource_type"] = resourceType
		}
		if nameParam != "" {
			if name := chi.URLParam(r, nameParam); name != "" {
				target["resource_name"] = name
			}
		}
		setRouteInfo(r.Context(), &RouteInfo{
			EventType: eventType,
			Target:    target,
		})
		h(w, r)
	}
}

// AuditedUpsert wraps a PUT handler whose event type depends on whether the
// operation created (201) or updated the resource.
func AuditedUpsert(onCreate, onUpdate, resourceType, nameParam string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := map[string]string{
			"method": r.Method,
			"path":   r.URL.Path,
		}
		if resourceType != "" {
			target["resource_type"] = resourceType
		}
		if nameParam != "" {
			if name := chi.URLParam(r, nameParam); name != "" {
				target["resource_name"] = name
			}
		}
		setRouteInfo(r.Context(), &RouteInfo{
			OnCreate: onCreate,
			OnUpdate: onUpdate,
			Target:   target,
		})
		h(w, r)
	}
}

// AuditedEntry wraps handlers on /entries/{type}/{name}/... paths.
// Reads "type", "name", and optionally "version" chi URL params.
func AuditedEntry(eventType string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := map[string]string{
			"method":        r.Method,
			"path":          r.URL.Path,
			"resource_type": ResourceTypeEntry,
		}
		if entryType := chi.URLParam(r, "type"); entryType != "" {
			target["entry_type"] = entryType
		}
		if name := chi.URLParam(r, "name"); name != "" {
			target["resource_name"] = name
		}
		if version := chi.URLParam(r, "version"); version != "" {
			target["version"] = version
		}
		setRouteInfo(r.Context(), &RouteInfo{
			EventType: eventType,
			Target:    target,
		})
		h(w, r)
	}
}

// AuditedServer wraps handlers on /{registryName}/v0.1/servers/... paths.
// Reads "registryName" and optionally "serverName" and "version" chi URL params.
func AuditedServer(eventType string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := map[string]string{
			"method":        r.Method,
			"path":          r.URL.Path,
			"resource_type": ResourceTypeServer,
		}
		if registryName := chi.URLParam(r, "registryName"); registryName != "" {
			target["registry_name"] = registryName
		}
		if serverName := chi.URLParam(r, "serverName"); serverName != "" {
			target["resource_name"] = serverName
		}
		if version := chi.URLParam(r, "version"); version != "" {
			target["version"] = version
		}
		setRouteInfo(r.Context(), &RouteInfo{
			EventType: eventType,
			Target:    target,
		})
		h(w, r)
	}
}

// AuditedSkill wraps handlers on .../skills/... paths.
// Reads "registryName", "namespace", "name", and optionally "version" params.
func AuditedSkill(eventType string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := map[string]string{
			"method":        r.Method,
			"path":          r.URL.Path,
			"resource_type": ResourceTypeSkill,
		}
		if registryName := chi.URLParam(r, "registryName"); registryName != "" {
			target["registry_name"] = registryName
		}
		if namespace := chi.URLParam(r, "namespace"); namespace != "" {
			target["namespace"] = namespace
		}
		if name := chi.URLParam(r, "name"); name != "" {
			target["resource_name"] = name
		}
		if version := chi.URLParam(r, "version"); version != "" {
			target["version"] = version
		}
		setRouteInfo(r.Context(), &RouteInfo{
			EventType: eventType,
			Target:    target,
		})
		h(w, r)
	}
}
