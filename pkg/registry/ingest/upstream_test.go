package ingest_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

func TestNewUpstreamRejectsNilClient(t *testing.T) {
	t.Parallel()
	require.NotPanics(t, func() {
		upstream, err := ingest.NewUpstream(nil)
		require.ErrorIs(t, err, persistence.ErrInvalid)
		require.Nil(t, upstream)
	})
}
