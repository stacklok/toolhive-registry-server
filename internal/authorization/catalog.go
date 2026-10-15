package authorization

import (
	"context"
	"net/http"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

//go:generate mockgen -destination=mocks/catalog_reader.go -package=mocks github.com/stacklok/toolhive-registry-server/pkg/registry/catalog Reader

// GuardCatalog never embeds raw capabilities: every method checks its own fixed
// action and private verified route evidence before touching the actual service.
func (g *Registrar) GuardCatalog(
	reader catalog.Reader, admin httpapi.AdminOperations,
) (httpapi.ConsumerPages, httpapi.AdminOperations, error) {
	if g == nil || g.engine == nil || reader == nil || admin.Sources == nil ||
		admin.Views == nil || admin.Publisher == nil || admin.Diagnostics == nil {
		return nil, httpapi.AdminOperations{}, ErrEvidence
	}
	guarded := &guardedCatalog{registrar: g, reader: reader, admin: admin}
	return guarded, httpapi.AdminOperations{Sources: guarded, Views: guarded, Publisher: guarded, Diagnostics: guarded}, nil
}

type guardedCatalog struct {
	registrar *Registrar
	reader    catalog.Reader
	admin     httpapi.AdminOperations
}

func (s *guardedCatalog) List(ctx context.Context, v string, q catalog.Query) (catalog.Page, error) {
	if err := s.registrar.admitCatalog(ctx, v, httpapi.List, q.Kind); err != nil {
		return catalog.Page{}, err
	}
	return s.reader.List(ctx, v, q)
}
func (s *guardedCatalog) Get(
	ctx context.Context, v string, n catalog.EntryName, q catalog.VersionSelector,
) (persistence.Entry, error) {
	if err := s.registrar.admitCatalog(ctx, v, httpapi.Get, n.Kind); err != nil {
		return persistence.Entry{}, err
	}
	return s.reader.Get(ctx, v, n, q)
}
func (s *guardedCatalog) ListVersions(
	ctx context.Context, v string, n catalog.EntryName, q catalog.PageRequest,
) (catalog.Page, error) {
	if err := s.registrar.admitCatalog(ctx, v, httpapi.ListVersions, n.Kind); err != nil {
		return catalog.Page{}, err
	}
	return s.reader.ListVersions(ctx, v, n, q)
}
func (s *guardedCatalog) CreateSource(ctx context.Context, d persistence.SourceDefinition) (persistence.SourceDefinition, error) {
	if err := s.registrar.admit(ctx, SourcesWrite); err != nil {
		return persistence.SourceDefinition{}, err
	}
	return s.admin.Sources.CreateSource(ctx, d)
}
func (s *guardedCatalog) UpdateSource(
	ctx context.Context, n string, d persistence.SourceDefinition,
) (persistence.SourceDefinition, error) {
	if err := s.registrar.admit(ctx, SourcesWrite); err != nil {
		return persistence.SourceDefinition{}, err
	}
	return s.admin.Sources.UpdateSource(ctx, n, d)
}
func (s *guardedCatalog) GetSource(ctx context.Context, n string) (persistence.SourceDefinition, error) {
	if err := s.registrar.admit(ctx, SourcesRead); err != nil {
		return persistence.SourceDefinition{}, err
	}
	return s.admin.Sources.GetSource(ctx, n)
}
func (s *guardedCatalog) ListSources(ctx context.Context) ([]persistence.SourceDefinition, error) {
	if err := s.registrar.admit(ctx, SourcesRead); err != nil {
		return nil, err
	}
	return s.admin.Sources.ListSources(ctx)
}
func (s *guardedCatalog) DeleteSource(ctx context.Context, n string) error {
	if err := s.registrar.admit(ctx, SourcesDelete); err != nil {
		return err
	}
	return s.admin.Sources.DeleteSource(ctx, n)
}
func (s *guardedCatalog) CreateView(ctx context.Context, d persistence.ViewDefinition) (persistence.ViewDefinition, error) {
	if err := s.registrar.admit(ctx, ViewsWrite); err != nil {
		return persistence.ViewDefinition{}, err
	}
	return s.admin.Views.CreateView(ctx, d)
}
func (s *guardedCatalog) UpdateView(
	ctx context.Context, n string, d persistence.ViewDefinition,
) (persistence.ViewDefinition, error) {
	if err := s.registrar.admit(ctx, ViewsWrite); err != nil {
		return persistence.ViewDefinition{}, err
	}
	return s.admin.Views.UpdateView(ctx, n, d)
}
func (s *guardedCatalog) GetView(ctx context.Context, n string) (persistence.ViewDefinition, error) {
	if err := s.registrar.admit(ctx, ViewsRead); err != nil {
		return persistence.ViewDefinition{}, err
	}
	return s.admin.Views.GetView(ctx, n)
}
func (s *guardedCatalog) ListViews(ctx context.Context) ([]persistence.ViewDefinition, error) {
	if err := s.registrar.admit(ctx, ViewsRead); err != nil {
		return nil, err
	}
	return s.admin.Views.ListViews(ctx)
}
func (s *guardedCatalog) DeleteView(ctx context.Context, n string) error {
	if err := s.registrar.admit(ctx, ViewsDelete); err != nil {
		return err
	}
	return s.admin.Views.DeleteView(ctx, n)
}
func (s *guardedCatalog) Publish(ctx context.Context, e persistence.Entry) (persistence.Entry, error) {
	if err := s.registrar.admit(ctx, EntriesPublish); err != nil {
		return persistence.Entry{}, err
	}
	return s.admin.Publisher.Publish(ctx, e)
}
func (s *guardedCatalog) DeleteManaged(ctx context.Context, k persistence.EntryKind, n, v string) error {
	if err := s.registrar.admit(ctx, EntriesDelete); err != nil {
		return err
	}
	return s.admin.Publisher.DeleteManaged(ctx, k, n, v)
}
func (s *guardedCatalog) ListSourceVersions(ctx context.Context, n string) ([]persistence.DiagnosticVersion, error) {
	if err := s.registrar.admit(ctx, SourcesRead); err != nil {
		return nil, err
	}
	return s.admin.Diagnostics.ListSourceVersions(ctx, n)
}
func (s *guardedCatalog) ListViewVersions(ctx context.Context, n string) ([]persistence.ViewDiagnosticVersion, error) {
	if err := s.registrar.admit(ctx, ViewsRead); err != nil {
		return nil, err
	}
	return s.admin.Diagnostics.ListViewVersions(ctx, n)
}
func (s *guardedCatalog) Status(ctx context.Context, n string) (persistence.JobStatus, error) {
	if err := s.registrar.admit(ctx, SourcesRead); err != nil {
		return persistence.JobStatus{}, err
	}
	return s.admin.Diagnostics.Status(ctx, n)
}

// DecorateCatalog validates a descriptor at the actual registration site and
// installs evidence. It does not infer authority from a raw request URL.
func (g *Registrar) DecorateCatalog(
	descriptor httpapi.RouteDescriptor, template string, action Action,
	handler http.Handler, authenticate func(http.Handler) http.Handler,
) (http.Handler, error) {
	method := descriptor.Method
	if handler == nil || authenticate == nil || !known(action) {
		return nil, ErrEvidence
	}
	key := method + " " + template
	if g.routes[key] {
		return nil, ErrEvidence
	}
	g.routes[key] = true
	return authenticate(g.wrap(method, template, action, descriptor, handler)), nil
}
