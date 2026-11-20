package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/api"
	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/claims"
	"github.com/stacklok/toolhive-registry-server/internal/service/mocks"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// The consumer-read logs below are v1.5.4's (internal/api/registry/v01 and
// internal/api/x/{skills,plugins} on v1.5.4): same messages, same fields.

// serveLogged serves one consumer GET with request IDs, optionally as an
// authenticated caller, and returns the slog records it wrote.
func serveLogged(
	t *testing.T, pages stubPages, managed api.ManagedSourceIDs, path string, caller jwt.MapClaims,
) (int, []map[string]any) {
	t.Helper()
	identify := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if caller != nil {
				r = r.WithContext(auth.ContextWithClaims(r.Context(), caller))
			}
			next.ServeHTTP(w, r)
		})
	}
	svc := mocks.NewMockRegistryService(gomock.NewController(t))
	server, err := api.NewServer(svc, pages, api.WithManagedSourceIDs(managed),
		api.WithMiddlewares(middleware.RequestID, identify))
	require.NoError(t, err)
	rr := httptest.NewRecorder()
	records := captureSlog(t, func() {
		server.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
	})
	return rr.Code, records
}

// recordsWith returns every record with message msg.
func recordsWith(records []map[string]any, msg string) []map[string]any {
	var out []map[string]any
	for _, r := range records {
		if r["msg"] == msg {
			out = append(out, r)
		}
	}
	return out
}

func consumerServer(name string) persistence.Entry {
	return persistence.Entry{SourceID: "synced", Kind: persistence.ServerKind, Name: name, Version: "1.0.0",
		Server: &model.Server{Schema: "https://example.org/s.json", Name: name, Version: "1.0.0", Description: "d"}}
}

// TestConsumerListLogs covers F8's success lines.
//
//nolint:paralleltest // captureSlog mutates slog.Default()
func TestConsumerListLogs(t *testing.T) {
	servers := stubPages{entries: []persistence.Entry{consumerServer("io.test/a"), consumerServer("io.test/b")}}

	t.Run("server list, authenticated", func(t *testing.T) {
		pages := servers
		pages.cursor = "next"
		code, records := serveLogged(t, pages, nil, "/registry/main/v0.1/servers",
			jwt.MapClaims{"sub": "alice", "name": "Alice"})
		require.Equal(t, http.StatusOK, code)
		got := recordsWith(records, "List servers completed")
		require.Len(t, got, 1)
		assert.Equal(t, "INFO", got[0]["level"])
		assert.EqualValues(t, 2, got[0]["result_count"])
		assert.Equal(t, true, got[0]["has_more"])
		assert.NotEmpty(t, got[0]["request_id"])
		assert.Equal(t, "alice", got[0]["sub"])
		assert.Equal(t, "Alice", got[0]["user"])
	})

	t.Run("server list, anonymous, last page", func(t *testing.T) {
		code, records := serveLogged(t, servers, nil, "/registry/main/v0.1/servers", nil)
		require.Equal(t, http.StatusOK, code)
		got := recordsWith(records, "List servers completed")
		require.Len(t, got, 1)
		assert.Equal(t, false, got[0]["has_more"])
		assert.Equal(t, auth.AnonymousSubject, got[0]["sub"])
		assert.Equal(t, "", got[0]["user"])
	})

	t.Run("server versions", func(t *testing.T) {
		code, records := serveLogged(t, servers, nil, "/registry/main/v0.1/servers/io.test%2Fa/versions",
			jwt.MapClaims{"sub": "alice", "email": "alice@example.org"})
		require.Equal(t, http.StatusOK, code)
		got := recordsWith(records, "List versions completed")
		require.Len(t, got, 1)
		assert.Equal(t, "INFO", got[0]["level"])
		assert.EqualValues(t, 2, got[0]["result_count"])
		assert.Equal(t, "io.test/a", got[0]["server_name"])
		assert.NotEmpty(t, got[0]["request_id"])
		assert.Equal(t, "alice", got[0]["sub"])
		assert.Equal(t, "alice@example.org", got[0]["user"])
		assert.Empty(t, recordsWith(records, "List servers completed"))
	})

	t.Run("skill list logs no server line", func(t *testing.T) {
		skill := persistence.Entry{SourceID: "synced", Kind: persistence.SkillKind, Name: "s", Version: "1.0.0",
			Skill: &model.Skill{Namespace: "io.test", Name: "s", Version: "1.0.0", Description: "d"}}
		code, records := serveLogged(t, stubPages{entries: []persistence.Entry{skill}}, nil,
			"/registry/main/v0.1/x/dev.toolhive/skills", nil)
		require.Equal(t, http.StatusOK, code)
		assert.Empty(t, recordsWith(records, "List servers completed"))
		assert.Empty(t, recordsWith(records, "List versions completed"))
	})
}

// TestConsumerUnexpectedErrorLogs covers F8's error line: every consumer 500
// logs "unexpected error" with the cause; mapped 4xx answers log nothing.
//
//nolint:paralleltest // captureSlog mutates slog.Default()
func TestConsumerUnexpectedErrorLogs(t *testing.T) {
	failingManaged := func(context.Context) (map[string]bool, error) { return nil, errors.New("db down") }
	skill := persistence.Entry{SourceID: "x", Kind: persistence.SkillKind, Name: "s", Version: "1",
		Skill: &model.Skill{Namespace: "io.test", Name: "s", Version: "1", Description: "d"}}

	tests := []struct {
		name      string
		pages     stubPages
		managed   api.ManagedSourceIDs
		path      string
		wantCode  int
		wantCause string
	}{
		{name: "server list storage failure", pages: stubPages{err: errors.New("pool exhausted")},
			path: "/registry/main/v0.1/servers", wantCode: http.StatusInternalServerError, wantCause: "pool exhausted"},
		{name: "server versions storage failure", pages: stubPages{err: errors.New("pool exhausted")},
			path:     "/registry/main/v0.1/servers/io.test%2Fa/versions",
			wantCode: http.StatusInternalServerError, wantCause: "pool exhausted"},
		{name: "skill list storage failure", pages: stubPages{err: errors.New("pool exhausted")},
			path:     "/registry/main/v0.1/x/dev.toolhive/skills",
			wantCode: http.StatusInternalServerError, wantCause: "pool exhausted"},
		{name: "managed source lookup failure", pages: stubPages{entries: []persistence.Entry{skill}},
			managed: failingManaged, path: "/registry/main/v0.1/x/dev.toolhive/skills",
			wantCode: http.StatusInternalServerError, wantCause: "failed to look up managed sources: db down"},
		{name: "not found", pages: stubPages{err: persistence.ErrNotFound},
			path: "/registry/main/v0.1/servers", wantCode: http.StatusNotFound},
		{name: "registry not found", pages: stubPages{err: persistence.ErrViewNotFound},
			path: "/registry/main/v0.1/servers", wantCode: http.StatusNotFound},
		{name: "forbidden", pages: stubPages{err: claims.ErrRegistryForbidden},
			path: "/registry/main/v0.1/servers", wantCode: http.StatusForbidden},
		{name: "invalid", pages: stubPages{err: persistence.ErrInvalid},
			path: "/registry/main/v0.1/servers", wantCode: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, records := serveLogged(t, tt.pages, tt.managed, tt.path, nil)
			require.Equal(t, tt.wantCode, code)
			got := recordsWith(records, "unexpected error")
			assert.Empty(t, recordsWith(records, "List servers completed"))
			if tt.wantCause == "" {
				assert.Empty(t, got)
				return
			}
			require.Len(t, got, 1)
			assert.Equal(t, "ERROR", got[0]["level"])
			assert.Equal(t, tt.wantCause, got[0]["error"])
		})
	}
}
