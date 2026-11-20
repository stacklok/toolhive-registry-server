package ingest_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
)

var _ reconcile.ChangeFetcher = (*ingest.Fetcher)(nil)

func hashOf(data string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(data))) }

func TestFetchIfChangedFile(t *testing.T) {
	t.Parallel()
	const invalid = `{"not":"a registry"}`
	for _, tc := range []struct {
		name          string
		content       string
		appliedHash   string
		wantUnchanged bool
		wantErr       error
	}{
		{name: "matching hash skips decoding", content: payload, appliedHash: hashOf(payload), wantUnchanged: true},
		// Content that would fail decoding proves the matching path never decodes.
		{name: "matching hash skips the acceptance policy", content: invalid, appliedHash: hashOf(invalid), wantUnchanged: true},
		{name: "different hash decodes", content: payload, appliedHash: hashOf("older")},
		{name: "no applied hash decodes", content: payload},
		{name: "different hash applies the acceptance policy", content: invalid, appliedHash: hashOf(payload), wantErr: persistence.ErrInvalid},
		{name: "no applied hash applies the acceptance policy", content: invalid, wantErr: persistence.ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := ingest.New(ingest.Options{ReadFile: func(context.Context, string) ([]byte, error) {
				return []byte(tc.content), nil
			}})
			source := persistence.SourceDefinition{Name: "file", File: &persistence.FileSpec{Path: "/host/registry.json"}, Schedule: "1h"}

			got, unchanged, err := f.FetchIfChanged(t.Context(), source, tc.appliedHash)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				require.False(t, unchanged)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantUnchanged, unchanged)
			require.Equal(t, hashOf(tc.content), got.Hash)
			if tc.wantUnchanged {
				require.Empty(t, got.Snapshot.Data.Servers, "an unchanged result carries no snapshot")
				return
			}
			require.Len(t, got.Snapshot.Data.Servers, 1)
		})
	}
}

func TestFetchIfChangedFileReadFailure(t *testing.T) {
	t.Parallel()
	f := ingest.New(ingest.Options{ReadFile: func(context.Context, string) ([]byte, error) {
		return nil, fmt.Errorf("secret path")
	}})
	source := persistence.SourceDefinition{Name: "file", File: &persistence.FileSpec{Path: "/host/registry.json"}, Schedule: "1h"}
	_, unchanged, err := f.FetchIfChanged(t.Context(), source, hashOf(payload))
	require.ErrorIs(t, err, ingest.ErrFetch)
	require.NotContains(t, err.Error(), "secret")
	require.False(t, unchanged)
}

func TestFetchIfChangedAPIAlwaysDecodes(t *testing.T) {
	t.Parallel()
	page := `{"servers":[{"server":{"$schema":"https://example.org/server.schema.json","name":"com.example/weather","description":"Weather","version":"1"}}]}`
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/openapi.yaml" {
			_, _ = w.Write([]byte("info:\n  version: 1.0.0\n  description: https://github.com/modelcontextprotocol/registry\n"))
			return
		}
		_, _ = w.Write([]byte(page))
	}))
	t.Cleanup(remote.Close)
	source := persistence.SourceDefinition{Name: "api", API: &persistence.APISpec{Endpoint: remote.URL, Timeout: "1s"}, Schedule: "1h"}
	f := ingest.New(ingest.Options{})

	first, err := f.Fetch(t.Context(), source)
	require.NoError(t, err)
	// API hashes cover decoded data, so even the applied hash is fetched in full.
	got, unchanged, err := f.FetchIfChanged(t.Context(), source, first.Hash)
	require.NoError(t, err)
	require.False(t, unchanged)
	require.Equal(t, first.Hash, got.Hash)
	require.Len(t, got.Snapshot.Data.Servers, 1)
}
