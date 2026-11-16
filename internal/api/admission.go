package api

import (
	"net/http"

	"github.com/stacklok/toolhive-registry-server/internal/audit"
	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/authorization"
)

const auditNameParam = "name"

type routeAdmission struct {
	method, template string
	action           authorization.Action
	retired          bool
	audit            routeAudit
}

type routeAudit struct {
	event, create, resource, param string
}

func (a routeAudit) wrap(handler http.Handler) http.Handler {
	// Metadata admission has its own decision audit.
	if a.event == "" {
		return handler
	}
	if a.create != "" {
		return audit.AuditedUpsert(a.create, a.event, a.resource, a.param, handler.ServeHTTP)
	}
	return audit.Audited(a.event, a.resource, a.param, handler.ServeHTTP)
}

func admissionManifest(admin bool) []routeAdmission {
	routes := []routeAdmission{{http.MethodGet, "/openapi.json", authorization.MetadataRead, false, routeAudit{}}}
	prefix := "/registry/{registryName}/v0.1"
	for _, route := range []struct{ suffix, event string }{
		{"/servers", audit.EventServerList},
		{"/servers/{serverName}/versions", audit.EventServerVersionsList},
		{"/servers/{serverName}/versions/{version}", audit.EventServerVersionRead},
	} {
		routes = append(routes, routeAdmission{http.MethodGet, prefix + route.suffix, authorization.CatalogRead, false,
			routeAudit{event: route.event, resource: audit.ResourceTypeServer, param: "serverName"}})
	}
	for _, family := range []struct {
		path, resource string
		events         [5]string
	}{
		{"skills", audit.ResourceTypeSkill, [5]string{
			audit.EventSkillList, audit.EventSkillList, audit.EventSkillRead, audit.EventSkillVersionsList, audit.EventSkillVersionRead,
		}},
		{"plugins", audit.ResourceTypePlugin, [5]string{
			audit.EventPluginList, audit.EventPluginList, audit.EventPluginRead,
			audit.EventPluginVersionsList, audit.EventPluginVersionRead,
		}},
	} {
		for i, suffix := range []string{
			"", "/", "/{namespace}/{name}", "/{namespace}/{name}/versions", "/{namespace}/{name}/versions/{version}",
		} {
			routes = append(routes, routeAdmission{
				http.MethodGet, prefix + "/x/dev.toolhive/" + family.path + suffix, authorization.CatalogRead, false,
				routeAudit{event: family.events[i], resource: family.resource, param: auditNameParam},
			})
		}
	}
	if !admin {
		return routes
	}
	for _, family := range []struct {
		path, resource      string
		read, write, delete authorization.Action
		events              [6]string
	}{
		{"sources", audit.ResourceTypeSource, authorization.SourcesRead, authorization.SourcesWrite, authorization.SourcesDelete,
			[6]string{audit.EventSourceList, audit.EventSourceRead, audit.EventSourceEntriesList,
				audit.EventSourceCreate, audit.EventSourceUpdate, audit.EventSourceDelete}},
		{"registries", audit.ResourceTypeRegistry, authorization.ViewsRead, authorization.ViewsWrite, authorization.ViewsDelete,
			[6]string{audit.EventRegistryList, audit.EventRegistryRead, audit.EventRegistryEntriesList,
				audit.EventRegistryCreate, audit.EventRegistryUpdate, audit.EventRegistryDelete}},
	} {
		path := "/v1/" + family.path
		routes = append(routes,
			routeAdmission{http.MethodGet, path, family.read, false,
				routeAudit{event: family.events[0], resource: family.resource}},
			routeAdmission{http.MethodGet, path + "/{name}", family.read, false,
				routeAudit{event: family.events[1], resource: family.resource, param: auditNameParam}},
			routeAdmission{http.MethodGet, path + "/{name}/entries", family.read, false,
				routeAudit{event: family.events[2], resource: family.resource, param: auditNameParam}},
			routeAdmission{http.MethodPut, path + "/{name}", family.write, false,
				routeAudit{event: family.events[4], create: family.events[3], resource: family.resource, param: auditNameParam}},
			routeAdmission{http.MethodDelete, path + "/{name}", family.delete, false,
				routeAudit{event: family.events[5], resource: family.resource, param: auditNameParam}})
	}
	return append(routes,
		routeAdmission{http.MethodPost, "/v1/entries", authorization.EntriesPublish, false,
			routeAudit{event: audit.EventEntryPublish, resource: audit.ResourceTypeEntry}},
		routeAdmission{http.MethodDelete, "/v1/entries/{type}/{name}/versions/{version}", authorization.EntriesDelete, false,
			routeAudit{event: audit.EventEntryDelete, resource: audit.ResourceTypeEntry, param: auditNameParam}},
		routeAdmission{http.MethodGet, "/v1/me", "", true, routeAudit{event: audit.EventUserInfo, resource: audit.ResourceTypeUser}},
		routeAdmission{http.MethodGet, "/v1/entries/{type}/{name}/claims", "", true,
			routeAudit{event: audit.EventEntryClaimsRead, resource: audit.ResourceTypeEntry, param: auditNameParam}},
		routeAdmission{http.MethodPut, "/v1/entries/{type}/{name}/claims", "", true,
			routeAudit{event: audit.EventEntryClaims, resource: audit.ResourceTypeEntry, param: auditNameParam}})
}

func retiredAdmissionHandler(w http.ResponseWriter, request *http.Request) {
	identity, established := auth.VerifiedIdentityFromContext(request.Context())
	if !established {
		http.Error(w, "authentication evidence missing", 500)
		return
	}
	if identity.Anonymous {
		auth.WriteAdmissionUnauthorized(w, request, "authentication required")
		return
	}
	http.Error(w, "legacy identity and entry-claims endpoints retired; see Cedar transition documentation", http.StatusGone)
}
