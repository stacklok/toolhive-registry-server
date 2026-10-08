package consumer

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence/conformance"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

type definitions interface {
	persistence.Sources
	persistence.Views
	persistence.Reconciler
}

func TestExternalAdapter(t *testing.T) {
	t.Parallel()
	var _ persistence.Definitions = (*postgres.Definitions)(nil)
	var _ conformance.Factory = func(*testing.T) persistence.Definitions { return nil }
	var _ conformance.PairFactory = func(*testing.T) (persistence.Definitions, persistence.Definitions) { return nil, nil }
	var _ persistence.Entries = (*postgres.Entries)(nil)
	var _ conformance.EntriesFactory = func(*testing.T) (persistence.Sources, persistence.Entries) { return nil, nil }
	if _, err := postgres.NewEntries(t.Context(), nil, 100); !errors.Is(err, persistence.ErrInvalid) {
		t.Fatalf("nil entries pool: %v", err)
	}
	if err := persistence.ValidateSnapshot(&model.Snapshot{}); err != nil {
		t.Fatalf("empty snapshot: %v", err)
	}
	if _, err := postgres.NewDefinitions(nil); !errors.Is(err, persistence.ErrInvalid) {
		t.Fatalf("nil pool: %v", err)
	}
}

func TestExternalContract(t *testing.T) {
	t.Parallel()
	var _ definitions = (persistence.Definitions)(nil)
	source := persistence.SourceDefinition{Name: "upstream", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"}
	kind, err := source.Kind()
	if err != nil || kind != "api" {
		t.Fatalf("kind = %q, error = %v", kind, err)
	}
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err = json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"claims", "tenant", "visibility"} {
		if _, ok := fields[key]; ok {
			t.Fatalf("unexpected policy key: %s", key)
		}
	}
}
