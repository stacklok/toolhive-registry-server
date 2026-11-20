package persistence_test

import (
	"reflect"
	"testing"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

const unusableSyncInline = `{"version":"1.0.0","meta":{"last_updated":"2025-01-01T00:00:00Z"},"data":{"servers":[]}}`

func TestWithoutUnusableSync(t *testing.T) {
	t.Parallel()
	filter := &persistence.Filter{Names: &persistence.NameFilter{Include: []string{"a*"}}}
	inline := &persistence.FileSpec{Data: unusableSyncInline}
	for _, tc := range []struct {
		name    string
		in      persistence.SourceDefinition
		want    persistence.SourceDefinition
		dropped []string
	}{
		{"polled source keeps both", persistence.SourceDefinition{Name: "g", Git: &persistence.GitSpec{Repository: "https://x"}, Schedule: "1h", Filter: filter},
			persistence.SourceDefinition{Name: "g", Git: &persistence.GitSpec{Repository: "https://x"}, Schedule: "1h", Filter: filter}, nil},
		{"inline drops schedule", persistence.SourceDefinition{Name: "i", File: inline, Schedule: "7s"},
			persistence.SourceDefinition{Name: "i", File: inline}, []string{"schedule"}},
		{"managed drops both", persistence.SourceDefinition{Name: "m", Managed: &persistence.ManagedSpec{}, Schedule: "1h", Filter: filter},
			persistence.SourceDefinition{Name: "m", Managed: &persistence.ManagedSpec{}}, []string{"schedule", "filter"}},
		{"kubernetes drops filter", persistence.SourceDefinition{Name: "k", Kubernetes: &persistence.KubernetesSpec{}, Filter: filter},
			persistence.SourceDefinition{Name: "k", Kubernetes: &persistence.KubernetesSpec{}}, []string{"filter"}},
		{"clean managed unchanged", persistence.SourceDefinition{Name: "m", Managed: &persistence.ManagedSpec{}},
			persistence.SourceDefinition{Name: "m", Managed: &persistence.ManagedSpec{}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, dropped := tc.in.WithoutUnusableSync()
			if !reflect.DeepEqual(got, tc.want) || !reflect.DeepEqual(dropped, tc.dropped) {
				t.Fatalf("got %+v %v; want %+v %v", got, dropped, tc.want, tc.dropped)
			}
			if _, err := got.Kind(); err != nil {
				t.Fatalf("cleaned definition is still invalid: %v", err)
			}
		})
	}
}
