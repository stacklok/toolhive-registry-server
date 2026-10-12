package ingest_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

const payload = `{"version":"1.0.0","meta":{"last_updated":"2025-01-15T10:30:00Z"},"data":{"servers":[{"$schema":"https://example.org/server.schema.json","name":"com.example/weather","description":"Weather","version":"1.0.0","packages":[]}],"skills":[{"namespace":"io.example","name":"skill","description":"Skill","version":"1.0.0"}],"plugins":[{"namespace":"io.example","name":"plugin","description":"Plugin","version":"1.0.0"}]}}`

func TestFileAndGit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	require.NoError(t, os.WriteFile(path, []byte(payload), 0600))
	repo, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	tree, err := repo.Worktree()
	require.NoError(t, err)
	_, err = tree.Add("registry.json")
	require.NoError(t, err)
	_, err = tree.Commit("fixture", &git.CommitOptions{Author: &object.Signature{Name: "fixture", Email: "fixture@example.org", When: time.Now()}})
	require.NoError(t, err)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(payload)) }))
	t.Cleanup(remote.Close)
	for _, tc := range []struct {
		name   string
		source persistence.SourceDefinition
	}{
		{"inline", persistence.SourceDefinition{Name: "inline", File: &persistence.FileSpec{Data: payload}}},
		{"path", persistence.SourceDefinition{Name: "path", File: &persistence.FileSpec{Path: path}, Schedule: "1h"}},
		{"url", persistence.SourceDefinition{Name: "url", File: &persistence.FileSpec{URL: remote.URL, Timeout: "1s"}, Schedule: "1h"}},
		{"git", persistence.SourceDefinition{Name: "git", Git: &persistence.GitSpec{Repository: dir}, Schedule: "1h"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := ingest.New(ingest.Options{})
			got, err := f.Fetch(t.Context(), tc.source)
			require.NoError(t, err)
			require.Len(t, got.Snapshot.Data.Servers, 1)
			require.Len(t, got.Snapshot.Data.Skills, 1)
			require.Len(t, got.Snapshot.Data.Plugins, 1)
			require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(payload))), got.Hash)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			_, err = f.Fetch(ctx, tc.source)
			require.ErrorIs(t, err, context.Canceled)
		})
	}
}

func TestInjectedIOAndSecretBoundary(t *testing.T) {
	t.Parallel()
	called := false
	f := ingest.New(ingest.Options{ReadFile: func(ctx context.Context, path string) ([]byte, error) {
		require.NoError(t, ctx.Err())
		require.Equal(t, "/host/registry.json", path)
		called = true
		return []byte(payload), nil
	}})
	_, err := f.Fetch(t.Context(), persistence.SourceDefinition{Name: "file", File: &persistence.FileSpec{Path: "/host/registry.json"}, Schedule: "1h"})
	require.NoError(t, err)
	require.True(t, called)

	source := persistence.SourceDefinition{Name: "git", Git: &persistence.GitSpec{Repository: "https://example.org/repo", Auth: &persistence.GitAuth{Username: "host", PasswordFile: "/host/credential"}}, Schedule: "1h"}
	_, err = ingest.New(ingest.Options{}).Fetch(t.Context(), source)
	require.ErrorIs(t, err, ingest.ErrFetch) // No implicit file/env credential loading.
	f = ingest.New(ingest.Options{LoadSecret: func(_ context.Context, path string) (string, error) {
		require.Equal(t, "/host/credential", path)
		return "", fmt.Errorf("secret detail: %w", context.Canceled)
	}})
	_, err = f.Fetch(t.Context(), source)
	require.ErrorIs(t, err, context.Canceled)
	require.NotContains(t, err.Error(), "secret detail")
	source.Git.Auth.PasswordFile = "relative"
	_, err = f.Fetch(t.Context(), source)
	require.ErrorIs(t, err, persistence.ErrInvalid)
}

func TestImportFailureNeverReturnsSnapshot(t *testing.T) {
	t.Parallel()
	for _, data := range []string{`{`, `{"data":{"servers":[]}}`, `{"version":"1.0.0","meta":{"last_updated":"2025-01-15T10:30:00Z"},"data":{"servers":[]}}`} {
		got, err := ingest.New(ingest.Options{}).Fetch(t.Context(), persistence.SourceDefinition{Name: "inline", File: &persistence.FileSpec{Data: data}})
		require.ErrorIs(t, err, persistence.ErrInvalid)
		require.Empty(t, got.Hash)
		require.Empty(t, got.Snapshot.Data.Servers)
	}
	_, err := ingest.New(ingest.Options{}).Fetch(t.Context(), persistence.SourceDefinition{Name: "missing", File: &persistence.FileSpec{Path: "/missing/secret-file"}, Schedule: "1h"})
	require.ErrorIs(t, err, ingest.ErrFetch)
	require.NotContains(t, err.Error(), "secret-file")
}

func TestAPIPagination(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"success", "page failure", "repeated cursor", "invalid payload", "missing array", "empty", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/openapi.yaml" {
					_, _ = w.Write([]byte("info:\n  version: 1.0.0\n  description: https://github.com/modelcontextprotocol/registry\n"))
					return
				}
				if mode == "cancel" {
					<-r.Context().Done()
					return
				}
				if mode == "empty" {
					_, _ = w.Write([]byte(`{"servers":[]}`))
					return
				}
				if r.URL.Query().Get("cursor") == "" {
					_, _ = w.Write([]byte(`{"servers":[{"server":{"$schema":"https://example.org/server.schema.json","name":"com.example/weather","description":"Weather","version":"1"}}],"metadata":{"nextCursor":"a&b"}}`))
					return
				}
				require.Equal(t, "a&b", r.URL.Query().Get("cursor"))
				switch mode {
				case "page failure":
					http.Error(w, "secret response", 500)
				case "repeated cursor":
					_, _ = w.Write([]byte(`{"servers":[],"metadata":{"nextCursor":"a&b"}}`))
				case "missing array":
					_, _ = w.Write([]byte(`{}`))
				case "invalid payload":
					_, _ = w.Write([]byte(`{"servers":[{"server":{"name":"bad"}}]}`))
				default:
					_, _ = w.Write([]byte(`{"servers":[{"server":{"$schema":"https://example.org/server.schema.json","name":"com.example/weather","description":"Weather","version":"2"}}]}`))
				}
			}))
			defer remote.Close()
			timeout := "1s"
			if mode == "cancel" {
				timeout = "20ms"
			}
			source := persistence.SourceDefinition{Name: "api", API: &persistence.APISpec{Endpoint: remote.URL, Timeout: timeout}, Schedule: "1h"}
			f := ingest.New(ingest.Options{})
			got, err := f.Fetch(t.Context(), source)
			if mode != "success" {
				require.Error(t, err)
				require.Empty(t, got.Hash)
				require.NotContains(t, err.Error(), "secret response")
				return
			}
			require.NoError(t, err)
			require.Len(t, got.Snapshot.Data.Servers, 2)
			again, err := f.Fetch(t.Context(), source)
			require.NoError(t, err)
			require.Equal(t, got.Hash, again.Hash)
		})
	}
}
