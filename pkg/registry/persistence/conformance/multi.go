package conformance

import (
	"errors"
	"sync"
	"testing"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// PairFactory constructs two distinct handles over one fresh, empty shared store.
type PairFactory func(*testing.T) (persistence.Definitions, persistence.Definitions)

// RunMulti checks cross-handle serialization and read consistency.
//
//nolint:gocyclo // The three independent race assertions share a factory and entry point.
func RunMulti(t *testing.T, factory PairFactory) {
	t.Helper()
	t.Run("managed singleton", func(t *testing.T) {
		a, b := factory(t)
		start := make(chan struct{})
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for _, item := range []struct {
			d    persistence.Definitions
			name string
		}{{a, "first"}, {b, "second"}} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, err := item.d.CreateSource(t.Context(), persistence.SourceDefinition{Name: item.name, Managed: &persistence.ManagedSpec{}})
				results <- err
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		successes := 0
		for err := range results {
			if err == nil {
				successes++
			} else {
				want(t, err, persistence.ErrConflict)
			}
		}
		if successes != 1 {
			t.Fatalf("created %d managed sources", successes)
		}
	})
	t.Run("reference versus delete", func(t *testing.T) {
		a, b := factory(t)
		_, err := a.CreateSource(t.Context(), persistence.SourceDefinition{Name: targetName, File: &persistence.FileSpec{Data: "{}"}})
		must(t, err)
		start := make(chan struct{})
		results := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, e := a.CreateView(t.Context(), persistence.ViewDefinition{Name: viewName, Sources: []string{targetName}})
			results <- e
		}()
		go func() { defer wg.Done(); <-start; results <- b.DeleteSource(t.Context(), targetName) }()
		close(start)
		wg.Wait()
		close(results)
		for e := range results {
			if e != nil && !errors.Is(e, persistence.ErrNotFound) && !errors.Is(e, persistence.ErrInUse) {
				t.Fatal(e)
			}
		}
		view, ve := b.GetView(t.Context(), viewName)
		if ve == nil {
			_, se := a.GetSource(t.Context(), targetName)
			if se != nil || len(view.Sources) != 1 || view.Sources[0] != targetName {
				t.Fatalf("dangling view: %+v / %v", view, se)
			}
		} else {
			want(t, ve, persistence.ErrNotFound)
		}
	})
	t.Run("config versus read", func(t *testing.T) {
		a, b := factory(t)
		old := persistence.SourceDefinition{Name: oldName, Managed: &persistence.ManagedSpec{}}
		newSource := persistence.SourceDefinition{Name: newName, Managed: &persistence.ManagedSpec{}}
		must(t, a.Reconcile(t.Context(), []persistence.SourceDefinition{old},
			[]persistence.ViewDefinition{{Name: viewName, Sources: []string{oldName}}}))
		start := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			<-start
			done <- a.Reconcile(t.Context(), []persistence.SourceDefinition{newSource},
				[]persistence.ViewDefinition{{Name: viewName, Sources: []string{newName}}})
		}()
		close(start)
		for i := 0; i < 30; i++ {
			all, err := b.ListSources(t.Context())
			must(t, err)
			if len(all) != 1 || all[0].Managed == nil || all[0].Name != oldName && all[0].Name != newName {
				t.Fatalf("intermediate managed set: %+v", all)
			}
			v, e := b.GetView(t.Context(), viewName)
			must(t, e)
			if len(v.Sources) != 1 || v.Sources[0] != oldName && v.Sources[0] != newName {
				t.Fatalf("mixed view: %+v", v)
			}
		}
		must(t, <-done)
		v, e := b.GetView(t.Context(), viewName)
		must(t, e)
		if len(v.Sources) != 1 || v.Sources[0] != newName {
			t.Fatal(v)
		}
	})
}
