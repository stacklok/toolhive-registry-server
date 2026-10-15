package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/authorization"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

func TestCatalogClassificationRejectsMismatchedDescriptor(t *testing.T) {
	t.Parallel()
	d := httpapi.RouteDescriptor{Method: "GET", Template: "/{registryName}/v0.1/servers", Kind: persistence.ServerKind, Operation: httpapi.List, AuditEvent: "server.list", ResourceType: "server", ResourceParam: "serverName", RegistryParam: "registryName"}
	e := routeAdmission{method: "GET", template: "/registry/{registryName}/v0.1/servers", action: authorization.CatalogRead, audit: routeAudit{event: "server.list", resource: "server", param: "serverName"}}
	require.True(t, matchesCatalogDescriptor(d, e))
	for _, change := range []func(*httpapi.RouteDescriptor){
		func(d *httpapi.RouteDescriptor) { d.Operation = httpapi.Delete },
		func(d *httpapi.RouteDescriptor) { d.Kind = persistence.SkillKind },
		func(d *httpapi.RouteDescriptor) { d.AuditEvent = "" },
		func(d *httpapi.RouteDescriptor) { d.ResourceType = "source" },
		func(d *httpapi.RouteDescriptor) { d.ResourceParam = "name" },
		func(d *httpapi.RouteDescriptor) { d.RegistryParam = "" },
	} {
		changed := d
		change(&changed)
		require.False(t, matchesCatalogDescriptor(changed, e), changed)
	}
	e.action = "unknown"
	require.False(t, matchesCatalogDescriptor(d, e))
}

func TestCatalogServerRequiresAdmissionAndAuthentication(t *testing.T) {
	t.Parallel()
	reader, ops := admissionCapabilities(t)
	_, err := NewCatalogServer(reader, ops, nil, false, func(h http.Handler) http.Handler { return h })
	require.ErrorIs(t, err, authorization.ErrEvidence)
	_, err = NewCatalogServer(reader, ops, admissionTestGate(t, admissionTestPermit), false, nil)
	require.ErrorIs(t, err, authorization.ErrEvidence)
}
