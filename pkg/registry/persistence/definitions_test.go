package persistence_test

import (
	"errors"
	"testing"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

const inlineRegistry = `{"version":"1.0.0","meta":{"last_updated":"2025-01-01T00:00:00Z"},"data":{"servers":[]}}`

func TestSourceKindScheduleAndFilterRules(t *testing.T) {
	t.Parallel()
	filter := &persistence.Filter{Names: &persistence.NameFilter{Include: []string{"io.example/*"}}}
	for _, tc := range []struct {
		name    string
		def     persistence.SourceDefinition
		invalid bool
	}{
		{"inline data with filter", persistence.SourceDefinition{Name: "inline", File: &persistence.FileSpec{Data: inlineRegistry}, Filter: filter}, false},
		{"inline data with schedule", persistence.SourceDefinition{Name: "inline", File: &persistence.FileSpec{Data: inlineRegistry}, Schedule: "1h"}, true},
		{"managed with filter", persistence.SourceDefinition{Name: "managed", Managed: &persistence.ManagedSpec{}, Filter: filter}, true},
		{"kubernetes with filter", persistence.SourceDefinition{Name: "k8s", Kubernetes: &persistence.KubernetesSpec{}, Filter: filter}, true},
		{"polled file with filter", persistence.SourceDefinition{Name: "file", File: &persistence.FileSpec{Path: "/r.json"}, Schedule: "1h", Filter: filter}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := tc.def.Kind()
			if tc.invalid != errors.Is(err, persistence.ErrInvalid) {
				t.Fatalf("Kind() = %v, want invalid=%v", err, tc.invalid)
			}
		})
	}
}
