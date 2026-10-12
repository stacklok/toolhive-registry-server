package conformance

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

//nolint:gocyclo,lll,goconst // Shared backend acceptance cases use explicit repeated fixture values.
func runDefinitionMetadata(t *testing.T, factory Factory) {
	t.Helper()
	t.Run("persisted timestamp metadata and rollback", func(t *testing.T) {
		t.Parallel()
		d, ctx := factory(t), t.Context()
		input := persistence.SourceDefinition{Name: "first", File: &persistence.FileSpec{Data: "{}"}}
		s, err := d.CreateSource(ctx, input)
		must(t, err)
		checkDefinitionTimes(t, s.CreatedAt, s.UpdatedAt)
		originalSource := s
		v, err := d.CreateView(ctx, persistence.ViewDefinition{Name: "view", Sources: []string{s.Name}})
		must(t, err)
		checkDefinitionTimes(t, v.CreatedAt, v.UpdatedAt)
		originalView := v
		// Ensure writes occur beyond even coarse timestamp resolution.
		time.Sleep(2 * time.Millisecond)
		// Metadata accepted on updates is never a client clock or compare-and-set.
		future := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
		s.CreatedAt, s.UpdatedAt = future, future
		updated, err := d.UpdateSource(ctx, s.Name, s)
		must(t, err)
		checkDefinitionTimes(t, updated.CreatedAt, updated.UpdatedAt)
		if !updated.CreatedAt.Equal(originalSource.CreatedAt) || !updated.UpdatedAt.After(originalSource.UpdatedAt) {
			t.Fatal("source update must preserve creation time and touch update time")
		}
		if updated.CreatedAt.Equal(future) || updated.UpdatedAt.Equal(future) {
			t.Fatal("accepted client timestamps")
		}
		got, err := d.GetSource(ctx, s.Name)
		must(t, err)
		if !reflect.DeepEqual(got, updated) {
			t.Fatal("update not persisted output")
		}
		v.CreatedAt, v.UpdatedAt = future, future
		uv, err := d.UpdateView(ctx, v.Name, v)
		must(t, err)
		if !uv.CreatedAt.Equal(originalView.CreatedAt) || !uv.UpdatedAt.After(originalView.UpdatedAt) {
			t.Fatal("view link replacement must preserve creation time and touch update time")
		}
		if uv.CreatedAt.Equal(future) || uv.UpdatedAt.Equal(future) {
			t.Fatal("accepted view client timestamps")
		}
		gotV, err := d.GetView(ctx, v.Name)
		must(t, err)
		if !reflect.DeepEqual(gotV, uv) {
			t.Fatal("view update not persisted output")
		}
		for range 2 {
			ss, e := d.ListSources(ctx)
			must(t, e)
			vs, e := d.ListViews(ctx)
			must(t, e)
			if !reflect.DeepEqual(ss, []persistence.SourceDefinition{updated}) || !reflect.DeepEqual(vs, []persistence.ViewDefinition{uv}) {
				t.Fatal("reads touched timestamps")
			}
		}
		bad := updated
		bad.ID = "stale-incarnation"
		_, err = d.UpdateSource(ctx, bad.Name, bad)
		want(t, err, persistence.ErrConflict)
		badV := uv
		badV.Sources = []string{"missing"}
		_, err = d.UpdateView(ctx, badV.Name, badV)
		want(t, err, persistence.ErrNotFound)
		want(t, d.DeleteSource(ctx, updated.Name), persistence.ErrInUse)
		got, err = d.GetSource(ctx, updated.Name)
		must(t, err)
		gotV, err = d.GetView(ctx, uv.Name)
		must(t, err)
		if !reflect.DeepEqual(got, updated) || !reflect.DeepEqual(gotV, uv) {
			t.Fatal("failed mutation changed metadata or links")
		}
		// Returned payloads and ordered links remain independent copies.
		got.File.Data = changedName
		gotV.Sources[0] = changedName
		got, err = d.GetSource(ctx, updated.Name)
		must(t, err)
		gotV, err = d.GetView(ctx, uv.Name)
		must(t, err)
		if !reflect.DeepEqual(got, updated) || !reflect.DeepEqual(gotV, uv) {
			t.Fatal("returned values alias storage")
		}
	})
	t.Run("config retained timestamps and atomic rollback", func(t *testing.T) {
		t.Parallel()
		d, ctx := factory(t), t.Context()
		config := []persistence.SourceDefinition{{Name: configName, File: &persistence.FileSpec{Data: "{}"}}}
		views := []persistence.ViewDefinition{{Name: "config-view", Sources: []string{configName}}}
		must(t, d.Reconcile(ctx, config, views))
		s, err := d.GetSource(ctx, configName)
		must(t, err)
		v, err := d.GetView(ctx, "config-view")
		must(t, err)
		checkDefinitionTimes(t, s.CreatedAt, s.UpdatedAt)
		checkDefinitionTimes(t, v.CreatedAt, v.UpdatedAt)
		api, err := d.CreateSource(ctx, persistence.SourceDefinition{Name: apiName, File: &persistence.FileSpec{Data: "{}"}})
		must(t, err)
		av, err := d.CreateView(ctx, persistence.ViewDefinition{Name: "metadata-api-view", Sources: []string{configName}})
		must(t, err)
		config[0].File.Data = "replacement"
		must(t, d.Reconcile(ctx, config, views))
		s2, err := d.GetSource(ctx, s.Name)
		must(t, err)
		v2, err := d.GetView(ctx, v.Name)
		must(t, err)
		if s2.ID != s.ID || !s2.CreatedAt.Equal(s.CreatedAt) || s2.UpdatedAt.Before(s.UpdatedAt) || !v2.CreatedAt.Equal(v.CreatedAt) || v2.UpdatedAt.Before(v.UpdatedAt) {
			t.Fatal("retained config metadata changed identity or creation time")
		}
		want(t, d.Reconcile(ctx, nil, nil), persistence.ErrInUse)
		s3, err := d.GetSource(ctx, s.Name)
		must(t, err)
		v3, err := d.GetView(ctx, v.Name)
		must(t, err)
		api2, err := d.GetSource(ctx, api.Name)
		must(t, err)
		av2, err := d.GetView(ctx, av.Name)
		must(t, err)
		if !reflect.DeepEqual(s2, s3) || !reflect.DeepEqual(v2, v3) || !reflect.DeepEqual(api, api2) || !reflect.DeepEqual(av, av2) {
			t.Fatal("rollback or reconcile touched unexpected timestamps")
		}
	})
	t.Run("create and reconcile reject assigned timestamps", func(t *testing.T) {
		t.Parallel()
		d, ctx := factory(t), t.Context()
		_, err := d.CreateSource(ctx, persistence.SourceDefinition{Name: targetName, File: &persistence.FileSpec{Data: "{}"}})
		must(t, err)
		for _, created := range []bool{true, false} {
			s := persistence.SourceDefinition{Name: newName, File: &persistence.FileSpec{Data: "{}"}}
			v := persistence.ViewDefinition{Name: "new-view", Sources: []string{targetName}}
			if created {
				s.CreatedAt = time.Now().UTC()
				v.CreatedAt = s.CreatedAt
			} else {
				s.UpdatedAt = time.Now().UTC()
				v.UpdatedAt = s.UpdatedAt
			}
			_, err = d.CreateSource(ctx, s)
			want(t, err, persistence.ErrInvalid)
			_, err = d.CreateView(ctx, v)
			want(t, err, persistence.ErrInvalid)
			want(t, d.Reconcile(ctx, []persistence.SourceDefinition{s}, nil), persistence.ErrInvalid)
			want(t, d.Reconcile(ctx, []persistence.SourceDefinition{{Name: targetName, File: &persistence.FileSpec{Data: "{}"}}}, []persistence.ViewDefinition{v}), persistence.ErrInvalid)
		}
		_, err = d.GetSource(ctx, newName)
		want(t, err, persistence.ErrNotFound)
		_, err = d.GetView(ctx, "new-view")
		want(t, err, persistence.ErrNotFound)
	})
	t.Run("authoritative conflict refinements", func(t *testing.T) {
		t.Parallel()
		d, ctx := factory(t), t.Context()
		managed := persistence.SourceDefinition{Name: "managed", Managed: &persistence.ManagedSpec{}}
		s, err := d.CreateSource(ctx, managed)
		must(t, err)
		_, err = d.CreateSource(ctx, managed)
		preciseConflict(t, err, persistence.ErrAlreadyExists)
		_, err = d.CreateSource(ctx, persistence.SourceDefinition{Name: "another", Managed: &persistence.ManagedSpec{}})
		preciseConflict(t, err, persistence.ErrConflict)
		_, err = d.CreateSource(ctx, persistence.SourceDefinition{Name: s.Name, File: &persistence.FileSpec{Data: "{}"}})
		preciseConflict(t, err, persistence.ErrAlreadyExists)
		_, err = d.UpdateSource(ctx, s.Name, persistence.SourceDefinition{Name: s.Name, File: &persistence.FileSpec{Data: "{}"}})
		preciseConflict(t, err, persistence.ErrKindChange)
		stale := s
		stale.ID = "stale"
		_, err = d.UpdateSource(ctx, s.Name, stale)
		preciseConflict(t, err, persistence.ErrConflict)
		v := persistence.ViewDefinition{Name: "view", Sources: []string{s.Name}}
		_, err = d.CreateView(ctx, v)
		must(t, err)
		_, err = d.CreateView(ctx, v)
		preciseConflict(t, err, persistence.ErrAlreadyExists)
		config := persistence.SourceDefinition{Name: configName, File: &persistence.FileSpec{Path: "/tmp/metadata"}, Schedule: "1h"}
		cv := persistence.ViewDefinition{Name: "config-view", Sources: []string{config.Name}}
		must(t, d.Reconcile(ctx, []persistence.SourceDefinition{config}, []persistence.ViewDefinition{cv}))
		cs, err := d.GetSource(ctx, config.Name)
		must(t, err)
		storedView, err := d.GetView(ctx, cv.Name)
		must(t, err)
		_, err = d.CreateSource(ctx, config)
		preciseConflict(t, err, persistence.ErrAlreadyExists)
		_, err = d.CreateView(ctx, cv)
		preciseConflict(t, err, persistence.ErrAlreadyExists)
		_, err = d.UpdateSource(ctx, cs.Name, cs)
		preciseConflict(t, err, persistence.ErrConfigOwned)
		_, err = d.UpdateView(ctx, cv.Name, storedView)
		preciseConflict(t, err, persistence.ErrConfigOwned)
		preciseConflict(t, d.DeleteSource(ctx, cs.Name), persistence.ErrConfigOwned)
		preciseConflict(t, d.DeleteView(ctx, cv.Name), persistence.ErrConfigOwned)
		config.File = &persistence.FileSpec{URL: "https://fixture.example.org"}
		preciseConflict(t, d.Reconcile(ctx, []persistence.SourceDefinition{config}, []persistence.ViewDefinition{cv}), persistence.ErrKindChange)
	})
}

func checkDefinitionTimes(t *testing.T, created, updated time.Time) {
	t.Helper()
	if created.IsZero() || updated.IsZero() || created.Location() != time.UTC ||
		updated.Location() != time.UTC || updated.Before(created) {
		t.Fatal("timestamps must be nonzero, storage-assigned UTC")
	}
}

func preciseConflict(t *testing.T, err, category error) {
	t.Helper()
	want(t, err, persistence.ErrConflict)
	for _, refined := range []error{persistence.ErrAlreadyExists, persistence.ErrConfigOwned, persistence.ErrKindChange} {
		if errors.Is(err, refined) != (category == refined) {
			t.Fatalf("incorrect refinement: %v (want %v)", err, category)
		}
	}
}
