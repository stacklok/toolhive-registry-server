package postgres_test

import (
	"testing"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi/conformance"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

func TestConsumerHTTPPostgres(t *testing.T) {
	t.Parallel()
	defs, pool := newBackend(t)
	store, err := postgres.NewEntries(t.Context(), pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	conformance.RunConsumer(t, defs, store)
}
