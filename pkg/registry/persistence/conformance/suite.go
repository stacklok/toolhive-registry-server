// Package conformance provides a backend-neutral definitions acceptance suite.
package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

const (
	gitName        = "git"
	apiName        = "api"
	fileName       = "file"
	oneName        = "one"
	configName     = "config"
	configViewName = "config-view"
	changedName    = "changed"
	targetName     = "target"
	oldName        = "old"
	newName        = "new"
	viewName       = "view"
)

// Factory must return a fresh empty backend for each test case.
type Factory func(*testing.T) persistence.Definitions

// Run exercises identical definition operations against each supplied backend.
//
//nolint:gocyclo // Table-driven suite deliberately groups the full contract in one entry point.
func Run(t *testing.T, newStore Factory) {
	t.Helper()
	mk := func(name string) persistence.SourceDefinition {
		return persistence.SourceDefinition{Name: name, Managed: &persistence.ManagedSpec{}}
	}
	cases := []struct {
		name string
		run  func(*testing.T, persistence.Definitions)
	}{
		{"roundtrip and order", func(t *testing.T, d persistence.Definitions) {
			t.Helper()
			ctx := t.Context()
			specs := []persistence.SourceDefinition{
				{Name: gitName, Git: &persistence.GitSpec{Repository: "https://example.org/repo"},
					Schedule: "1h", Filter: &persistence.Filter{Names: &persistence.NameFilter{Include: []string{"a"}}}},
				{Name: apiName, API: &persistence.APISpec{Endpoint: "https://example.org", Timeout: "30s"}, Schedule: "1m"},
				{Name: fileName, File: &persistence.FileSpec{Path: "/tmp/registry.json"}, Schedule: "1s"},
				{Name: testInlineName, File: &persistence.FileSpec{Data: "{}"}},
				{Name: "kubernetes", Kubernetes: &persistence.KubernetesSpec{Namespaces: []string{oneName}}}, mk(testManagedKind),
			}
			for _, s := range specs {
				made, e := d.CreateSource(ctx, s)
				must(t, e)
				if made.ID == "" || made.Origin != persistence.OriginAPI {
					t.Fatalf("identity/origin: %+v", made)
				}
				got, e := d.GetSource(ctx, s.Name)
				must(t, e)
				if !reflect.DeepEqual(got, made) {
					t.Fatalf("roundtrip: %#v != %#v", got, made)
				}
				data, e := json.Marshal(got)
				must(t, e)
				if string(data) == "" {
					t.Fatal("empty JSON")
				}
				var m map[string]any
				must(t, json.Unmarshal(data, &m))
				if _, ok := m["claims"]; ok {
					t.Fatal("public contract contains claims")
				}
			}
			all, e := d.ListSources(ctx)
			must(t, e)
			original, e := d.GetSource(ctx, gitName)
			must(t, e)
			updated, e := d.UpdateSource(ctx, gitName, persistence.SourceDefinition{
				Name: gitName, Git: &persistence.GitSpec{Repository: "https://example.org/new"}, Schedule: "2h",
			})
			must(t, e)
			if updated.ID != original.ID || updated.Git.Repository != "https://example.org/new" {
				t.Fatalf("update changed identity or ignored replacement: %+v", updated)
			}
			original.Git.Repository = "https://example.org/returned"
			updated, e = d.UpdateSource(ctx, gitName, original)
			must(t, e)
			if updated.ID != original.ID || updated.Git.Repository != original.Git.Repository {
				t.Fatalf("read-modify-write: %+v", updated)
			}
			original.ID = "different"
			_, e = d.UpdateSource(ctx, gitName, original)
			want(t, e, persistence.ErrConflict)
			_, e = d.UpdateSource(ctx, fileName, persistence.SourceDefinition{
				Name: fileName, File: &persistence.FileSpec{URL: "https://example.org/file.json"}, Schedule: "1s",
			})
			want(t, e, persistence.ErrConflict)
			if len(all) != len(specs) {
				t.Fatalf("sources: %+v", all)
			}
			for i, n := range []string{apiName, fileName, gitName, testInlineName, "kubernetes", testManagedKind} {
				if all[i].Name != n {
					t.Fatalf("list order: %+v", all)
				}
			}
			view, e := d.CreateView(ctx, persistence.ViewDefinition{Name: "v", Sources: []string{gitName, apiName}})
			must(t, e)
			if view.Origin != persistence.OriginAPI {
				t.Fatal(view)
			}
			got, e := d.GetView(ctx, "v")
			must(t, e)
			if !reflect.DeepEqual(got.Sources, []string{gitName, apiName}) {
				t.Fatal(got)
			}
			_, e = d.UpdateView(ctx, "v", persistence.ViewDefinition{Name: "v", Sources: []string{apiName, gitName}})
			must(t, e)
			got, e = d.GetView(ctx, "v")
			must(t, e)
			if !reflect.DeepEqual(got.Sources, []string{apiName, gitName}) {
				t.Fatal(got)
			}
			got.Origin = persistence.OriginConfig
			_, e = d.UpdateView(ctx, "v", got)
			want(t, e, persistence.ErrInvalid)
			got.Origin = persistence.OriginAPI
			got.Sources = []string{apiName, gitName}
			_, e = d.UpdateView(ctx, "v", got)
			must(t, e)
			e = d.DeleteSource(ctx, gitName)
			want(t, e, persistence.ErrInUse)
			must(t, d.DeleteView(ctx, "v"))
			must(t, d.DeleteSource(ctx, gitName))
		}},
		{"invalid and immutable", func(t *testing.T, d persistence.Definitions) {
			t.Helper()
			ctx := t.Context()
			for _, s := range []persistence.SourceDefinition{
				{Name: "Upper", Managed: &persistence.ManagedSpec{}}, {Name: "empty"},
				{Name: "multi", Managed: &persistence.ManagedSpec{}, File: &persistence.FileSpec{Data: "{}"}},
				{Name: gitName, Git: &persistence.GitSpec{Repository: "r"}},
			} {
				_, e := d.CreateSource(ctx, s)
				want(t, e, persistence.ErrInvalid)
			}
			_, e := d.CreateSource(ctx, mk(oneName))
			must(t, e)
			_, e = d.CreateSource(ctx, mk("two"))
			want(t, e, persistence.ErrConflict)
			_, e = d.CreateSource(ctx, mk(oneName))
			want(t, e, persistence.ErrConflict)
			_, e = d.UpdateSource(ctx, oneName, persistence.SourceDefinition{
				Name: oneName, File: &persistence.FileSpec{Path: "/tmp/file.json"}, Schedule: "1s",
			})
			want(t, e, persistence.ErrConflict)
			_, e = d.UpdateSource(ctx, oneName, mk("other"))
			want(t, e, persistence.ErrInvalid)
			_, e = d.GetSource(ctx, "missing")
			want(t, e, persistence.ErrNotFound)
			_, e = d.CreateView(ctx, persistence.ViewDefinition{Name: "v", Sources: []string{oneName, oneName}})
			want(t, e, persistence.ErrInvalid)
			_, e = d.CreateView(ctx, persistence.ViewDefinition{Name: "v", Sources: []string{"missing"}})
			want(t, e, persistence.ErrNotFound)
			if !strings.Contains(e.Error(), "missing") {
				t.Fatalf("missing reference name: %v", e)
			}
			_, e = d.GetView(ctx, "missing")
			want(t, e, persistence.ErrNotFound)
			want(t, d.DeleteSource(ctx, "missing"), persistence.ErrNotFound)
		}},
		{"config reconcile rollback and API preservation", func(t *testing.T, d persistence.Definitions) {
			t.Helper()
			ctx := t.Context()
			must(t, d.Reconcile(ctx,
				[]persistence.SourceDefinition{mk(configName)},
				[]persistence.ViewDefinition{{Name: configViewName, Sources: []string{configName}}}))
			s, e := d.GetSource(ctx, configName)
			must(t, e)
			if s.Origin != persistence.OriginConfig {
				t.Fatal(s)
			}
			must(t, d.Reconcile(ctx, []persistence.SourceDefinition{mk(configName)},
				[]persistence.ViewDefinition{{Name: configViewName, Sources: []string{configName}}}))
			stable, e := d.GetSource(ctx, configName)
			must(t, e)
			if stable.ID != s.ID {
				t.Fatalf("no-op changed identity: %+v", stable)
			}
			views, e := d.ListViews(ctx)
			must(t, e)
			if len(views) != 1 || views[0].Name != configViewName {
				t.Fatalf("views: %+v", views)
			}
			_, e = d.UpdateSource(ctx, configName, mk(configName))
			want(t, e, persistence.ErrConflict)
			_, e = d.UpdateView(ctx, configViewName, persistence.ViewDefinition{Name: configViewName, Sources: []string{configName}})
			want(t, e, persistence.ErrConflict)
			_, e = d.CreateSource(ctx, persistence.SourceDefinition{
				Name: apiName, Git: &persistence.GitSpec{Repository: "r"}, Schedule: "1s",
			})
			must(t, e)
			_, e = d.CreateView(ctx, persistence.ViewDefinition{Name: "api-view", Sources: []string{configName}})
			must(t, e)
			must(t, d.Reconcile(ctx, []persistence.SourceDefinition{mk(configName)},
				[]persistence.ViewDefinition{{Name: configViewName, Sources: []string{configName}}}))
			apiView, e := d.GetView(ctx, "api-view")
			must(t, e)
			if apiView.Origin != persistence.OriginAPI || len(apiView.Sources) != 1 || apiView.Sources[0] != configName {
				t.Fatalf("config reconcile changed API view: %+v", apiView)
			}
			err := d.Reconcile(ctx, nil, nil)
			want(t, err, persistence.ErrInUse)
			got, e := d.GetSource(ctx, configName)
			must(t, e)
			if got.ID != s.ID {
				t.Fatalf("rollback lost source identity: %+v", got)
			}
			_, e = d.GetView(ctx, configViewName)
			must(t, e)
			err = d.Reconcile(ctx, []persistence.SourceDefinition{mk(newName),
				{Name: apiName, Git: &persistence.GitSpec{Repository: "r"}, Schedule: "1s"}}, nil)
			want(t, err, persistence.ErrConflict)
			_, e = d.GetSource(ctx, newName)
			want(t, e, persistence.ErrNotFound)
			must(t, d.DeleteView(ctx, "api-view"))
			must(t, d.Reconcile(ctx, nil, nil))
			_, e = d.GetSource(ctx, configName)
			want(t, e, persistence.ErrNotFound)
			_, e = d.GetSource(ctx, apiName)
			must(t, e)
		}},
		{"file config location immutable", func(t *testing.T, d persistence.Definitions) {
			t.Helper()
			ctx := t.Context()
			file := persistence.SourceDefinition{Name: "file", File: &persistence.FileSpec{Path: "/tmp/a"}, Schedule: "1s"}
			must(t, d.Reconcile(ctx, []persistence.SourceDefinition{file}, nil))
			file.File = &persistence.FileSpec{Data: "{}"}
			file.Schedule = ""
			want(t, d.Reconcile(ctx, []persistence.SourceDefinition{file}, nil), persistence.ErrConflict)
			got, err := d.GetSource(ctx, "file")
			must(t, err)
			if got.File.Path != "/tmp/a" {
				t.Fatal(got)
			}
		}},
		{"config managed replacement", func(t *testing.T, d persistence.Definitions) {
			t.Helper()
			ctx := t.Context()
			must(t, d.Reconcile(ctx, []persistence.SourceDefinition{mk(oldName)},
				[]persistence.ViewDefinition{{Name: viewName, Sources: []string{oldName}}}))
			must(t, d.Reconcile(ctx, []persistence.SourceDefinition{mk(newName)},
				[]persistence.ViewDefinition{{Name: viewName, Sources: []string{newName}}}))
			_, err := d.GetSource(ctx, oldName)
			want(t, err, persistence.ErrNotFound)
			v, err := d.GetView(ctx, viewName)
			must(t, err)
			if !reflect.DeepEqual(v.Sources, []string{newName}) {
				t.Fatal(v)
			}
		}},
		{"API managed conflicts with config", func(t *testing.T, d persistence.Definitions) {
			t.Helper()
			_, err := d.CreateSource(t.Context(), mk("api-managed"))
			must(t, err)
			want(t, d.Reconcile(t.Context(), []persistence.SourceDefinition{mk("config-managed")}, nil), persistence.ErrConflict)
		}},
		{"API view name collision", func(t *testing.T, d persistence.Definitions) {
			t.Helper()
			ctx := t.Context()
			_, err := d.CreateSource(ctx, persistence.SourceDefinition{Name: "api", File: &persistence.FileSpec{Data: "{}"}})
			must(t, err)
			_, err = d.CreateView(ctx, persistence.ViewDefinition{Name: "collision", Sources: []string{"api"}})
			must(t, err)
			want(t, d.Reconcile(ctx, []persistence.SourceDefinition{{Name: "config", File: &persistence.FileSpec{Data: "{}"}}},
				[]persistence.ViewDefinition{{Name: "collision", Sources: []string{"config"}}}), persistence.ErrConflict)
			_, err = d.GetSource(ctx, "config")
			want(t, err, persistence.ErrNotFound)
		}},
		{"replacement rollback on API reference", func(t *testing.T, d persistence.Definitions) {
			t.Helper()
			ctx := t.Context()
			must(t, d.Reconcile(ctx, []persistence.SourceDefinition{mk(oldName)},
				[]persistence.ViewDefinition{{Name: configViewName, Sources: []string{oldName}}}))
			old, err := d.GetSource(ctx, oldName)
			must(t, err)
			_, err = d.CreateView(ctx, persistence.ViewDefinition{Name: "api-view", Sources: []string{oldName}})
			must(t, err)
			want(t, d.Reconcile(ctx, []persistence.SourceDefinition{mk(newName)},
				[]persistence.ViewDefinition{{Name: configViewName, Sources: []string{newName}}}), persistence.ErrInUse)
			got, err := d.GetSource(ctx, oldName)
			must(t, err)
			if got.ID != old.ID {
				t.Fatalf("changed source: %+v", got)
			}
			_, err = d.GetSource(ctx, newName)
			want(t, err, persistence.ErrNotFound)
			v, err := d.GetView(ctx, "config-view")
			must(t, err)
			if len(v.Sources) != 1 || v.Sources[0] != oldName {
				t.Fatal(v)
			}
		}},
		{"view lexical byte order", func(t *testing.T, d persistence.Definitions) {
			t.Helper()
			_, err := d.CreateSource(t.Context(), persistence.SourceDefinition{Name: "source", File: &persistence.FileSpec{Data: "{}"}})
			must(t, err)
			for _, name := range []string{"é", "a", "Z", "Ä"} {
				_, err := d.CreateView(t.Context(), persistence.ViewDefinition{Name: name, Sources: []string{"source"}})
				must(t, err)
			}
			views, err := d.ListViews(t.Context())
			must(t, err)
			for i, name := range []string{"Z", "a", "Ä", "é"} {
				if views[i].Name != name {
					t.Fatalf("order: %+v", views)
				}
			}
		}},
		{"cancellation and alias safety", func(t *testing.T, d persistence.Definitions) {
			t.Helper()
			canceled, cancel := context.WithCancel(t.Context())
			cancel()
			_, e := d.CreateSource(canceled, mk("no"))
			want(t, e, context.Canceled)
			_, e = d.GetSource(canceled, "no")
			want(t, e, context.Canceled)
			ctx := t.Context()
			names := []string{"ns"}
			src := persistence.SourceDefinition{Name: "k", Kubernetes: &persistence.KubernetesSpec{Namespaces: names}}
			_, e = d.CreateSource(ctx, src)
			must(t, e)
			names[0] = changedName
			got, e := d.GetSource(ctx, "k")
			must(t, e)
			if got.Kubernetes.Namespaces[0] != "ns" {
				t.Fatal(got)
			}
			got.Kubernetes.Namespaces[0] = "mutated"
			got, e = d.GetSource(ctx, "k")
			must(t, e)
			if got.Kubernetes.Namespaces[0] != "ns" {
				t.Fatal(got)
			}
			refs := []string{"k"}
			_, e = d.CreateView(ctx, persistence.ViewDefinition{Name: "v", Sources: refs})
			must(t, e)
			refs[0] = changedName
			v, e := d.GetView(ctx, "v")
			must(t, e)
			if v.Sources[0] != "k" {
				t.Fatal(v)
			}
			v.Sources[0] = changedName
			v, e = d.GetView(ctx, "v")
			must(t, e)
			if v.Sources[0] != "k" {
				t.Fatal(v)
			}
		}},
		{"concurrent singleton and reference race", func(t *testing.T, d persistence.Definitions) {
			t.Helper()
			ctx := t.Context()
			var wg sync.WaitGroup
			results := make(chan error, 2)
			for _, n := range []string{oneName, "two"} {
				wg.Add(1)
				go func() { defer wg.Done(); _, e := d.CreateSource(ctx, mk(n)); results <- e }()
			}
			wg.Wait()
			close(results)
			success := 0
			for e := range results {
				if e == nil {
					success++
				} else {
					want(t, e, persistence.ErrConflict)
				}
			}
			if success != 1 {
				t.Fatalf("managed creates: %d", success)
			}
			_, e := d.CreateSource(ctx, persistence.SourceDefinition{
				Name: targetName, File: &persistence.FileSpec{Data: "{}"},
			})
			must(t, e)
			results = make(chan error, 2)
			wg.Add(2)
			go func() {
				defer wg.Done()
				_, e := d.CreateView(ctx, persistence.ViewDefinition{Name: "r", Sources: []string{targetName}})
				results <- e
			}()
			go func() { defer wg.Done(); results <- d.DeleteSource(ctx, targetName) }()
			wg.Wait()
			close(results)
			for e := range results {
				if e != nil && !errors.Is(e, persistence.ErrNotFound) && !errors.Is(e, persistence.ErrInUse) {
					t.Fatal(e)
				}
			}
			_, se := d.GetSource(ctx, targetName)
			v, ve := d.GetView(ctx, "r")
			if ve == nil && (se != nil || len(v.Sources) != 1 || v.Sources[0] != targetName) {
				t.Fatalf("dangling view: %+v / %v", v, se)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { t.Parallel(); tc.run(t, newStore(t)) })
	}
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func want(t *testing.T, e, target error) {
	t.Helper()
	if !errors.Is(e, target) {
		t.Fatalf("error %v, want %v", e, target)
	}
}
