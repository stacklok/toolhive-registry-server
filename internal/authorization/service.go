package authorization

import (
	"context"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"

	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// guardedService deliberately does not embed RegistryService: no promoted bypasses.
type guardedService struct {
	raw       service.RegistryService
	registrar *Registrar
}

// Guard returns only explicit guarded operations to HTTP; readiness stays on its own seam.
func (g *Registrar) Guard(raw service.RegistryService) (service.RegistryService, error) {
	if raw == nil || g == nil || g.engine == nil {
		return nil, ErrEvidence
	}
	return &guardedService{raw: raw, registrar: g}, nil
}

func (*guardedService) CheckReadiness(context.Context) error { return ErrEvidence }
func (*guardedService) GetEntryClaims(context.Context, ...service.Option) (map[string]any, error) {
	return nil, ErrEvidence
}
func (*guardedService) UpdateEntryClaims(context.Context, ...service.Option) error {
	return ErrEvidence
}

func (s *guardedService) ListServers(ctx context.Context, opts ...service.Option) (*service.ListServersResult, error) {
	if err := s.registrar.admit(ctx, CatalogRead); err != nil {
		return nil, err
	}
	return s.raw.ListServers(ctx, opts...)
}
func (s *guardedService) ListServerVersions(ctx context.Context, opts ...service.Option) ([]*upstreamv0.ServerJSON, error) {
	if err := s.registrar.admit(ctx, CatalogRead); err != nil {
		return nil, err
	}
	return s.raw.ListServerVersions(ctx, opts...)
}
func (s *guardedService) GetServerVersion(ctx context.Context, opts ...service.Option) (*upstreamv0.ServerJSON, error) {
	if err := s.registrar.admit(ctx, CatalogRead); err != nil {
		return nil, err
	}
	return s.raw.GetServerVersion(ctx, opts...)
}
func (s *guardedService) ListSkills(ctx context.Context, opts ...service.Option) (*service.ListSkillsResult, error) {
	if err := s.registrar.admit(ctx, CatalogRead); err != nil {
		return nil, err
	}
	return s.raw.ListSkills(ctx, opts...)
}
func (s *guardedService) GetSkillVersion(ctx context.Context, opts ...service.Option) (*service.Skill, error) {
	if err := s.registrar.admit(ctx, CatalogRead); err != nil {
		return nil, err
	}
	return s.raw.GetSkillVersion(ctx, opts...)
}
func (s *guardedService) ListPlugins(ctx context.Context, opts ...service.Option) (*service.ListPluginsResult, error) {
	if err := s.registrar.admit(ctx, CatalogRead); err != nil {
		return nil, err
	}
	return s.raw.ListPlugins(ctx, opts...)
}
func (s *guardedService) GetPluginVersion(ctx context.Context, opts ...service.Option) (*service.Plugin, error) {
	if err := s.registrar.admit(ctx, CatalogRead); err != nil {
		return nil, err
	}
	return s.raw.GetPluginVersion(ctx, opts...)
}
func (s *guardedService) PublishServerVersion(ctx context.Context, opts ...service.Option) (*upstreamv0.ServerJSON, error) {
	if err := s.registrar.admit(ctx, EntriesPublish); err != nil {
		return nil, err
	}
	return s.raw.PublishServerVersion(ctx, opts...)
}
func (s *guardedService) PublishSkill(ctx context.Context, skill *service.Skill, opts ...service.Option) (*service.Skill, error) {
	if err := s.registrar.admit(ctx, EntriesPublish); err != nil {
		return nil, err
	}
	return s.raw.PublishSkill(ctx, skill, opts...)
}
func (s *guardedService) PublishPlugin(ctx context.Context, plugin *service.Plugin,
	opts ...service.Option) (*service.Plugin, error) {
	if err := s.registrar.admit(ctx, EntriesPublish); err != nil {
		return nil, err
	}
	return s.raw.PublishPlugin(ctx, plugin, opts...)
}
func (s *guardedService) DeleteServerVersion(ctx context.Context, opts ...service.Option) error {
	if err := s.registrar.admit(ctx, EntriesDelete); err != nil {
		return err
	}
	return s.raw.DeleteServerVersion(ctx, opts...)
}
func (s *guardedService) DeleteSkillVersion(ctx context.Context, opts ...service.Option) error {
	if err := s.registrar.admit(ctx, EntriesDelete); err != nil {
		return err
	}
	return s.raw.DeleteSkillVersion(ctx, opts...)
}
func (s *guardedService) DeletePluginVersion(ctx context.Context, opts ...service.Option) error {
	if err := s.registrar.admit(ctx, EntriesDelete); err != nil {
		return err
	}
	return s.raw.DeletePluginVersion(ctx, opts...)
}
func (s *guardedService) ListSources(ctx context.Context) ([]service.SourceInfo, error) {
	if err := s.registrar.admit(ctx, SourcesRead); err != nil {
		return nil, err
	}
	return s.raw.ListSources(ctx)
}
func (s *guardedService) GetSourceByName(ctx context.Context, name string) (*service.SourceInfo, error) {
	if err := s.registrar.admit(ctx, SourcesRead); err != nil {
		return nil, err
	}
	return s.raw.GetSourceByName(ctx, name)
}
func (s *guardedService) ListSourceEntries(ctx context.Context, name string) ([]service.SourceEntryInfo, error) {
	if err := s.registrar.admit(ctx, SourcesRead); err != nil {
		return nil, err
	}
	return s.raw.ListSourceEntries(ctx, name)
}
func (s *guardedService) CreateSource(ctx context.Context, name string, req *service.SourceCreateRequest,
) (*service.SourceInfo, error) {
	if err := s.registrar.admit(ctx, SourcesWrite); err != nil {
		return nil, err
	}
	return s.raw.CreateSource(ctx, name, req)
}
func (s *guardedService) UpdateSource(ctx context.Context, name string, req *service.SourceCreateRequest,
) (*service.SourceInfo, error) {
	if err := s.registrar.admit(ctx, SourcesWrite); err != nil {
		return nil, err
	}
	return s.raw.UpdateSource(ctx, name, req)
}
func (s *guardedService) ProcessInlineSourceData(ctx context.Context, name, data string) error {
	if err := s.registrar.admit(ctx, SourcesWrite); err != nil {
		return err
	}
	return s.raw.ProcessInlineSourceData(ctx, name, data)
}
func (s *guardedService) DeleteSource(ctx context.Context, name string) error {
	if err := s.registrar.admit(ctx, SourcesDelete); err != nil {
		return err
	}
	return s.raw.DeleteSource(ctx, name)
}
func (s *guardedService) ListRegistries(ctx context.Context) ([]service.RegistryInfo, error) {
	if err := s.registrar.admit(ctx, ViewsRead); err != nil {
		return nil, err
	}
	return s.raw.ListRegistries(ctx)
}
func (s *guardedService) GetRegistryByName(ctx context.Context, name string) (*service.RegistryInfo, error) {
	if err := s.registrar.admit(ctx, ViewsRead); err != nil {
		return nil, err
	}
	return s.raw.GetRegistryByName(ctx, name)
}
func (s *guardedService) ListRegistryEntries(ctx context.Context, name string) ([]service.RegistryEntryInfo, error) {
	if err := s.registrar.admit(ctx, ViewsRead); err != nil {
		return nil, err
	}
	return s.raw.ListRegistryEntries(ctx, name)
}
func (s *guardedService) CreateRegistry(ctx context.Context, name string, req *service.RegistryCreateRequest,
) (*service.RegistryInfo, error) {
	if err := s.registrar.admit(ctx, ViewsWrite); err != nil {
		return nil, err
	}
	return s.raw.CreateRegistry(ctx, name, req)
}
func (s *guardedService) UpdateRegistry(ctx context.Context, name string, req *service.RegistryCreateRequest,
) (*service.RegistryInfo, error) {
	if err := s.registrar.admit(ctx, ViewsWrite); err != nil {
		return nil, err
	}
	return s.raw.UpdateRegistry(ctx, name, req)
}
func (s *guardedService) DeleteRegistry(ctx context.Context, name string) error {
	if err := s.registrar.admit(ctx, ViewsDelete); err != nil {
		return err
	}
	return s.raw.DeleteRegistry(ctx, name)
}
