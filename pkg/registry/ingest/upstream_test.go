package ingest_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/httpclient"
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

func TestUpstreamValidateProbesServerList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		body          string
		expectError   string
	}{
		{
			name: "accepts a v0.1 server list without OpenAPI",
			body: `{"servers":[{"server":{"name":"io.example/a","description":"d","version":"1.0.0"},"_meta":{}}],"metadata":{"count":1}}`,
		},
		{name: "rejects a missing servers array", body: `{"items":[]}`, expectError: "missing a 'servers' array"},
		{name: "rejects an invalid server list", body: `{"servers":["io.example/a"]}`, expectError: "not a valid server list"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v0.1/servers", r.URL.Path)
				assert.Equal(t, "1", r.URL.Query().Get("limit"))
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, tt.body)
			}))
			defer server.Close()

			upstream, err := ingest.NewUpstream(httpclient.NewDefaultClient(0))
			require.NoError(t, err)
			err = upstream.Validate(context.Background(), server.URL)
			if tt.expectError != "" {
				require.ErrorContains(t, err, tt.expectError)
				return
			}
			require.NoError(t, err)
		})
	}
}