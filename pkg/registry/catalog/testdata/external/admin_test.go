package consumer

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// These independent fixture capabilities intentionally support a single inline
// API source and a single view, not a production backend or shared conformance.
type ownSources struct {
	source persistence.SourceDefinition
	view   *ownViews
}
type ownViews struct {
	view    persistence.ViewDefinition
	sources *ownSources
}

func (b *ownSources) CreateSource(ctx context.Context, s persistence.SourceDefinition) (persistence.SourceDefinition, error) {
	if err := ctx.Err(); err != nil {
		return s, err
	}
	if _, err := s.Kind(); err != nil {
		return s, err
	}
	if s.ID != "" || s.Origin != "" || !s.CreatedAt.IsZero() || !s.UpdatedAt.IsZero() || s.File == nil || s.File.Data == "" {
		return s, persistence.ErrInvalid
	}
	if b.source.Name == s.Name {
		return s, persistence.ErrAlreadyExists
	}
	if b.source.Name != "" {
		return s, persistence.ErrConflict
	}
	s.ID, s.Origin = "host-incarnation", persistence.OriginAPI
	s.CreatedAt = time.Now().UTC()
	s.UpdatedAt = s.CreatedAt
	b.source = copySource(s)
	return copySource(b.source), nil
}
func (b *ownSources) GetSource(ctx context.Context, name string) (persistence.SourceDefinition, error) {
	if err := ctx.Err(); err != nil {
		return persistence.SourceDefinition{}, err
	}
	if name == "" || b.source.Name != name {
		return persistence.SourceDefinition{}, persistence.ErrNotFound
	}
	return copySource(b.source), nil
}
func (b *ownSources) ListSources(ctx context.Context) ([]persistence.SourceDefinition, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.source.Name == "" {
		return []persistence.SourceDefinition{}, nil
	}
	return []persistence.SourceDefinition{copySource(b.source)}, nil
}
func (b *ownSources) UpdateSource(ctx context.Context, name string, s persistence.SourceDefinition) (persistence.SourceDefinition, error) {
	old, err := b.GetSource(ctx, name)
	if err != nil {
		return s, err
	}
	if s.Name != name || s.Origin != "" && s.Origin != persistence.OriginAPI {
		return s, persistence.ErrInvalid
	}
	if s.ID != "" && s.ID != old.ID {
		return s, persistence.ErrConflict
	}
	if _, err = s.Kind(); err != nil {
		return s, err
	}
	if s.File == nil || s.File.Data == "" {
		return s, persistence.ErrKindChange
	}
	s.ID, s.Origin, s.CreatedAt, s.UpdatedAt = old.ID, old.Origin, old.CreatedAt, time.Now().UTC()
	b.source = copySource(s)
	return copySource(s), nil
}
func (b *ownSources) DeleteSource(ctx context.Context, name string) error {
	if _, err := b.GetSource(ctx, name); err != nil {
		return err
	}
	if b.view.view.Name != "" {
		return persistence.ErrInUse
	}
	b.source = persistence.SourceDefinition{}
	return nil
}
func copySource(s persistence.SourceDefinition) persistence.SourceDefinition {
	if s.File != nil {
		file := *s.File
		s.File = &file
	}
	return s
}
func copyView(v persistence.ViewDefinition) persistence.ViewDefinition {
	v.Sources = append([]string(nil), v.Sources...)
	return v
}
func (b *ownViews) CreateView(ctx context.Context, v persistence.ViewDefinition) (persistence.ViewDefinition, error) {
	if v.Origin != "" || !v.CreatedAt.IsZero() || !v.UpdatedAt.IsZero() {
		return v, persistence.ErrInvalid
	}
	if err := persistence.ValidateView(v); err != nil {
		return v, err
	}
	if b.view.Name == v.Name {
		return v, persistence.ErrAlreadyExists
	}
	if b.view.Name != "" {
		return v, persistence.ErrConflict
	}
	for _, name := range v.Sources {
		if _, err := b.sources.GetSource(ctx, name); err != nil {
			return v, err
		}
	}
	v.Origin, v.CreatedAt = persistence.OriginAPI, time.Now().UTC()
	v.UpdatedAt = v.CreatedAt
	b.view = copyView(v)
	return copyView(v), nil
}
func (b *ownViews) GetView(ctx context.Context, name string) (persistence.ViewDefinition, error) {
	if err := ctx.Err(); err != nil {
		return persistence.ViewDefinition{}, err
	}
	if name == "" || name != b.view.Name {
		return persistence.ViewDefinition{}, persistence.ErrNotFound
	}
	return copyView(b.view), nil
}
func (b *ownViews) ListViews(ctx context.Context) ([]persistence.ViewDefinition, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.view.Name == "" {
		return []persistence.ViewDefinition{}, nil
	}
	return []persistence.ViewDefinition{copyView(b.view)}, nil
}
func (b *ownViews) UpdateView(ctx context.Context, name string, v persistence.ViewDefinition) (persistence.ViewDefinition, error) {
	old, err := b.GetView(ctx, name)
	if err != nil {
		return v, err
	}
	if v.Name != name || v.Origin != "" && v.Origin != persistence.OriginAPI {
		return v, persistence.ErrInvalid
	}
	if err = persistence.ValidateView(v); err != nil {
		return v, err
	}
	for _, source := range v.Sources {
		if _, err = b.sources.GetSource(ctx, source); err != nil {
			return v, err
		}
	}
	v.Origin, v.CreatedAt, v.UpdatedAt = old.Origin, old.CreatedAt, time.Now().UTC()
	b.view = copyView(v)
	return copyView(v), nil
}
func (b *ownViews) DeleteView(ctx context.Context, name string) error {
	if _, err := b.GetView(ctx, name); err != nil {
		return err
	}
	b.view = persistence.ViewDefinition{}
	return nil
}

// A host may replace the application implementation altogether. Every operation
// is an explicit method, so the consumer depends only on DefinitionAdmin.
type ownAdmin struct {
	sources *ownSources
	views   *ownViews
}

var _ catalog.DefinitionAdmin = ownAdmin{}

func (a ownAdmin) CreateSource(c context.Context, s persistence.SourceDefinition) (persistence.SourceDefinition, error) {
	return a.sources.CreateSource(c, s)
}
func (a ownAdmin) GetSource(c context.Context, n string) (persistence.SourceDefinition, error) {
	return a.sources.GetSource(c, n)
}
func (a ownAdmin) ListSources(c context.Context) ([]persistence.SourceDefinition, error) {
	return a.sources.ListSources(c)
}
func (a ownAdmin) UpdateSource(c context.Context, n string, s persistence.SourceDefinition) (persistence.SourceDefinition, error) {
	return a.sources.UpdateSource(c, n, s)
}
func (a ownAdmin) DeleteSource(c context.Context, n string) error {
	return a.sources.DeleteSource(c, n)
}
func (a ownAdmin) CreateView(c context.Context, v persistence.ViewDefinition) (persistence.ViewDefinition, error) {
	return a.views.CreateView(c, v)
}
func (a ownAdmin) GetView(c context.Context, n string) (persistence.ViewDefinition, error) {
	return a.views.GetView(c, n)
}
func (a ownAdmin) ListViews(c context.Context) ([]persistence.ViewDefinition, error) {
	return a.views.ListViews(c)
}
func (a ownAdmin) UpdateView(c context.Context, n string, v persistence.ViewDefinition) (persistence.ViewDefinition, error) {
	return a.views.UpdateView(c, n, v)
}
func (a ownAdmin) DeleteView(c context.Context, n string) error { return a.views.DeleteView(c, n) }

func TestIndependentDefinitionsAndReplacementApplication(t *testing.T) {
	t.Parallel()
	for _, implementation := range []string{"default", "replacement"} {
		t.Run(implementation, func(t *testing.T) {
			t.Parallel()
			sources, views := &ownSources{}, &ownViews{}
			sources.view, views.sources = views, sources
			var admin catalog.DefinitionAdmin
			var err error
			if implementation == "replacement" {
				admin = ownAdmin{sources: sources, views: views}
			} else {
				admin, err = catalog.NewDefinitionAdmin(sources, views, catalog.DefinitionAdminOptions{})
				if err != nil {
					t.Fatal(err)
				}
			}
			exerciseDefinitions(t, admin)
		})
	}
}
func exerciseDefinitions(t *testing.T, admin catalog.DefinitionAdmin) {
	t.Helper()
	ctx := t.Context()
	s, err := admin.CreateSource(ctx, persistence.SourceDefinition{Name: "inline", File: &persistence.FileSpec{Data: "{}"}})
	if err != nil || s.CreatedAt.IsZero() || s.UpdatedAt.IsZero() || s.ID == "" {
		t.Fatalf("create: %+v %v", s, err)
	}
	got, err := admin.GetSource(ctx, s.Name)
	if err != nil || !reflect.DeepEqual(s, got) {
		t.Fatal("get source")
	}
	ss, err := admin.ListSources(ctx)
	if err != nil || !reflect.DeepEqual(ss, []persistence.SourceDefinition{s}) {
		t.Fatal("list sources")
	}
	s.File.Data = "replacement"
	s, err = admin.UpdateSource(ctx, s.Name, s)
	if err != nil || s.File.Data != "replacement" {
		t.Fatal("update source")
	}
	v, err := admin.CreateView(ctx, persistence.ViewDefinition{Name: "catalog", Sources: []string{s.Name}})
	if err != nil || v.CreatedAt.IsZero() {
		t.Fatal("create view")
	}
	gv, err := admin.GetView(ctx, v.Name)
	if err != nil || !reflect.DeepEqual(v, gv) {
		t.Fatal("get view")
	}
	vs, err := admin.ListViews(ctx)
	if err != nil || !reflect.DeepEqual(vs, []persistence.ViewDefinition{v}) {
		t.Fatal("list views")
	}
	if _, err = admin.UpdateView(ctx, v.Name, v); err != nil {
		t.Fatal(err)
	}
	if err = admin.DeleteSource(ctx, s.Name); !errors.Is(err, persistence.ErrInUse) {
		t.Fatal(err)
	}
	if err = admin.DeleteView(ctx, v.Name); err != nil {
		t.Fatal(err)
	}
	if err = admin.DeleteSource(ctx, s.Name); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.GetSource(ctx, s.Name); !errors.Is(err, persistence.ErrNotFound) {
		t.Fatal(err)
	}
}
