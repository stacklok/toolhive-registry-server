package ingest_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

func fileRegistry(servers ...string) string {
	return `{"version":"1.0.0","meta":{"last_updated":"2025-01-15T10:30:00Z"},"data":{"servers":[` + strings.Join(servers, ",") + `]}}`
}

func fileServer(name, description, extra string) string {
	return `{"$schema":"https://example.org/server.schema.json","name":"` + name + `","description":"` + description +
		`","version":"1.0.0"` + extra + `}`
}

// File and Git sources keep v1.5.4's acceptance: the toolhive-core wire schema
// plus a required name and description, not the stricter publication rules.
func TestFilePolicyMatchesV154(t *testing.T) {
	t.Parallel()
	remote := `,"remotes":[{"type":"streamable-http","url":"https://example.org/mcp"},{"type":"streamable-http","url":"https://example.org/mcp"}]`
	for _, tc := range []struct {
		name    string
		data    string
		invalid bool
	}{
		{name: "name ending in underscore", data: fileRegistry(fileServer("com.example/bar_", "ok", ""))},
		{name: "namespace starting with a dash", data: fileRegistry(fileServer("-example.com/x", "ok", ""))},
		{name: "duplicate remotes", data: fileRegistry(fileServer("com.example/dup", "ok", remote))},
		{name: "missing description", data: fileRegistry(fileServer("com.example/x", "", "")), invalid: true},
		{name: "no servers", data: fileRegistry(), invalid: true},
		{name: "schema violation", data: fileRegistry(fileServer("no-slash", "ok", "")), invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ingest.New(ingest.Options{}).Fetch(t.Context(),
				persistence.SourceDefinition{Name: "inline", File: &persistence.FileSpec{Data: tc.data}})
			if tc.invalid {
				require.ErrorIs(t, err, persistence.ErrInvalid)
				require.NotEmpty(t, ingest.Cause(err), "the reason is kept for the sync failure log")
				return
			}
			require.NoError(t, err)
			require.NoError(t, persistence.ValidateStorableSnapshot(&got.Snapshot))
		})
	}
}

// api.timeout bounds each request, not the whole paginated fetch.
func TestAPITimeoutIsPerRequest(t *testing.T) {
	t.Parallel()
	const pages = 4
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		page := 0
		_, _ = fmt.Sscan(r.URL.Query().Get("cursor"), &page)
		next := ""
		if page+1 < pages {
			next = fmt.Sprint(page + 1)
		}
		_, _ = fmt.Fprintf(w, `{"servers":[{"server":{"name":"com.example/s%d","description":"d","version":"1"}}],"metadata":{"nextCursor":%q}}`, page, next)
	}))
	t.Cleanup(remote.Close)
	source := persistence.SourceDefinition{Name: "api", API: &persistence.APISpec{Endpoint: remote.URL, Timeout: "400ms"}, Schedule: "1h"}
	started := time.Now()
	got, err := ingest.New(ingest.Options{}).Fetch(t.Context(), source)
	require.NoError(t, err)
	require.Greater(t, time.Since(started), 400*time.Millisecond, "the whole fetch outlasts a single request timeout")
	require.Len(t, got.Snapshot.Data.Servers, pages)

	slow := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { time.Sleep(time.Second) }))
	t.Cleanup(slow.Close)
	source.API.Endpoint = slow.URL
	_, err = ingest.New(ingest.Options{}).Fetch(t.Context(), source)
	require.ErrorIs(t, err, ingest.ErrFetch, "a single slow request still times out")
}

// A fetch failure keeps a redacted cause for logs while its message stays the
// sanitized sentinel.
func TestFetchFailureCause(t *testing.T) {
	t.Parallel()
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "secret response", http.StatusForbidden)
	}))
	t.Cleanup(remote.Close)
	endpoint := strings.Replace(remote.URL, "http://", "http://user:hunter2@", 1) + "/path?token=abc"
	_, err := ingest.New(ingest.Options{}).Fetch(t.Context(),
		persistence.SourceDefinition{Name: "url", File: &persistence.FileSpec{URL: endpoint}, Schedule: "1h"})
	require.ErrorIs(t, err, ingest.ErrFetch)
	require.Equal(t, ingest.ErrFetch.Error(), err.Error())
	cause := ingest.Cause(err)
	require.Contains(t, cause, "HTTP 403")
	require.Contains(t, cause, strings.TrimPrefix(remote.URL, "http://")+"/path")
	for _, secret := range []string{"user", "hunter2", "token", "abc", "secret response"} {
		require.NotContains(t, cause, secret)
	}
}

func TestRedactURLs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"no url here", "no url here"},
		{`Get "https://user:***@example.org/a?token=x#frag": dial tcp`, `Get "https://example.org/a": dial tcp`},
		{"clone https://bot:pat@github.com/org/repo.git failed.", "clone https://github.com/org/repo.git failed."},
		{"HTTP 500 for URL http://example.org/v0.1/servers?limit=100&cursor=abc: oops", "HTTP 500 for URL http://example.org/v0.1/servers: oops"},
		{"file:///etc/registry.json", "file:///etc/registry.json"},
	} {
		require.Equal(t, tc.want, ingest.RedactURLs(tc.in), tc.in)
	}
}

func TestWithCause(t *testing.T) {
	t.Parallel()
	require.NoError(t, ingest.WithCause(nil, errors.New("x")))
	require.Equal(t, ingest.ErrFetch, ingest.WithCause(ingest.ErrFetch, nil))
	err := fmt.Errorf("wrapped: %w", ingest.WithCause(persistence.ErrInvalid, errors.New("see https://u:p@h/x?q=1")))
	require.ErrorIs(t, err, persistence.ErrInvalid)
	require.Equal(t, "see https://h/x", ingest.Cause(err))
	require.Empty(t, ingest.Cause(persistence.ErrInvalid))
}
