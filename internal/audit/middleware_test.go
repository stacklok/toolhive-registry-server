package audit

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// newTestLogger creates a Logger that writes to the provided buffer.
// Since tests are in the same package, we can access unexported fields.
func newTestLogger(buf *bytes.Buffer) *Logger {
	handler := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: auditLevel})
	return &Logger{logger: slog.New(handler)}
}

// enabledConfig returns a minimal enabled AuditConfig for testing.
func enabledConfig() *config.AuditConfig {
	return &config.AuditConfig{Enabled: true}
}

// withRouteInfo is a test helper that simulates an audited* wrapper by writing
// the given RouteInfo into the middleware-injected carrier. It must be used as
// a layer between Middleware and the actual inner handler.
func withRouteInfo(info *RouteInfo, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setRouteInfo(r.Context(), info)
		h.ServeHTTP(w, r)
	})
}

// --- Middleware tests ---

func TestMiddleware_Disabled(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name   string
		cfg    *config.AuditConfig
		logger *Logger
	}{
		{name: "nil config", cfg: nil, logger: newTestLogger(&bytes.Buffer{})},
		{name: "disabled config", cfg: &config.AuditConfig{Enabled: false}, logger: newTestLogger(&bytes.Buffer{})},
		{name: "nil logger", cfg: enabledConfig(), logger: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := Middleware(tt.cfg, tt.logger)(inner)
			req := httptest.NewRequest(http.MethodPut, "/v1/sources/test", nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusOK, rec.Code)
		})
	}
}

func TestMiddleware_SkipsUnannotatedPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
	}{
		{name: "health endpoint", path: "/health"},
		{name: "non-v1 registry path", path: "/registry/something"},
		{name: "unknown v1 path", path: "/v1/unknown-endpoint"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			logger := newTestLogger(&buf)

			handler := Middleware(enabledConfig(), logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodDelete, tt.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Empty(t, buf.String(), "unannotated path should not produce audit events")
		})
	}
}

func TestMiddleware_EmitsAuditEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		method           string
		path             string
		innerStatus      int
		routeInfo        *RouteInfo
		claims           jwt.MapClaims
		expectEventType  string
		expectOutcome    string
		expectSubjectSub string
		expectAnonymous  bool
	}{
		{
			name:        "PUT source with 200 emits source.update",
			method:      http.MethodPut,
			path:        "/v1/sources/my-source",
			innerStatus: http.StatusOK,
			routeInfo: &RouteInfo{
				OnCreate: EventSourceCreate,
				OnUpdate: EventSourceUpdate,
				Target:   map[string]string{"method": http.MethodPut, "path": "/v1/sources/my-source", "resource_type": ResourceTypeSource, "resource_name": "my-source"},
			},
			claims:           jwt.MapClaims{"sub": "user-123"},
			expectEventType:  EventSourceUpdate,
			expectOutcome:    "success",
			expectSubjectSub: "user-123",
		},
		{
			name:        "PUT source with 201 emits source.create",
			method:      http.MethodPut,
			path:        "/v1/sources/my-source",
			innerStatus: http.StatusCreated,
			routeInfo: &RouteInfo{
				OnCreate: EventSourceCreate,
				OnUpdate: EventSourceUpdate,
				Target:   map[string]string{"method": http.MethodPut, "path": "/v1/sources/my-source", "resource_type": ResourceTypeSource, "resource_name": "my-source"},
			},
			claims:           jwt.MapClaims{"sub": "user-123"},
			expectEventType:  EventSourceCreate,
			expectOutcome:    "success",
			expectSubjectSub: "user-123",
		},
		{
			name:        "PUT registry with 201 emits registry.create",
			method:      http.MethodPut,
			path:        "/v1/registries/my-reg",
			innerStatus: http.StatusCreated,
			routeInfo: &RouteInfo{
				OnCreate: EventRegistryCreate,
				OnUpdate: EventRegistryUpdate,
				Target:   map[string]string{"method": http.MethodPut, "path": "/v1/registries/my-reg", "resource_type": ResourceTypeRegistry, "resource_name": "my-reg"},
			},
			claims:           jwt.MapClaims{"sub": "user-456"},
			expectEventType:  EventRegistryCreate,
			expectOutcome:    "success",
			expectSubjectSub: "user-456",
		},
		{
			name:        "DELETE source returns 403",
			method:      http.MethodDelete,
			path:        "/v1/sources/my-source",
			innerStatus: http.StatusForbidden,
			routeInfo: &RouteInfo{
				EventType: EventSourceDelete,
				Target:    map[string]string{"method": http.MethodDelete, "path": "/v1/sources/my-source", "resource_type": ResourceTypeSource, "resource_name": "my-source"},
			},
			claims:          jwt.MapClaims{"sub": "blocked-user"},
			expectEventType: EventSourceDelete,
			expectOutcome:   "denied",
		},
		{
			name:        "POST entry with server error",
			method:      http.MethodPost,
			path:        "/v1/entries",
			innerStatus: http.StatusInternalServerError,
			routeInfo: &RouteInfo{
				EventType: EventEntryPublish,
				Target:    map[string]string{"method": http.MethodPost, "path": "/v1/entries", "resource_type": ResourceTypeEntry},
			},
			claims:          jwt.MapClaims{"sub": "user-456"},
			expectEventType: EventEntryPublish,
			expectOutcome:   "error",
		},
		{
			name:        "DELETE registry with 404 failure",
			method:      http.MethodDelete,
			path:        "/v1/registries/my-reg",
			innerStatus: http.StatusNotFound,
			routeInfo: &RouteInfo{
				EventType: EventRegistryDelete,
				Target:    map[string]string{"method": http.MethodDelete, "path": "/v1/registries/my-reg", "resource_type": ResourceTypeRegistry, "resource_name": "my-reg"},
			},
			claims:          jwt.MapClaims{"sub": "user-789"},
			expectEventType: EventRegistryDelete,
			expectOutcome:   "failure",
		},
		{
			name:        "anonymous user",
			method:      http.MethodPut,
			path:        "/v1/sources/anon-source",
			innerStatus: http.StatusOK,
			routeInfo: &RouteInfo{
				OnCreate: EventSourceCreate,
				OnUpdate: EventSourceUpdate,
				Target:   map[string]string{"method": http.MethodPut, "path": "/v1/sources/anon-source", "resource_type": ResourceTypeSource, "resource_name": "anon-source"},
			},
			claims:          nil,
			expectEventType: EventSourceUpdate,
			expectOutcome:   "success",
			expectAnonymous: true,
		},
		{
			name:        "DELETE entry version",
			method:      http.MethodDelete,
			path:        "/v1/entries/tool/my-tool/versions/1.0.0",
			innerStatus: http.StatusNoContent,
			routeInfo: &RouteInfo{
				EventType: EventEntryDelete,
				Target:    map[string]string{"method": http.MethodDelete, "path": "/v1/entries/tool/my-tool/versions/1.0.0", "resource_type": ResourceTypeEntry, "entry_type": "tool", "resource_name": "my-tool", "version": "1.0.0"},
			},
			claims:          jwt.MapClaims{"sub": "user-del"},
			expectEventType: EventEntryDelete,
			expectOutcome:   "success",
		},
		{
			name:        "PUT entry claims",
			method:      http.MethodPut,
			path:        "/v1/entries/tool/my-tool/claims",
			innerStatus: http.StatusOK,
			routeInfo: &RouteInfo{
				EventType: EventEntryClaims,
				Target:    map[string]string{"method": http.MethodPut, "path": "/v1/entries/tool/my-tool/claims", "resource_type": ResourceTypeEntry, "entry_type": "tool", "resource_name": "my-tool"},
			},
			claims:          jwt.MapClaims{"sub": "admin-1"},
			expectEventType: EventEntryClaims,
			expectOutcome:   "success",
		},
		{
			name:        "GET source emits source.read",
			method:      http.MethodGet,
			path:        "/v1/sources/my-source",
			innerStatus: http.StatusOK,
			routeInfo: &RouteInfo{
				EventType: EventSourceRead,
				Target:    map[string]string{"method": http.MethodGet, "path": "/v1/sources/my-source", "resource_type": ResourceTypeSource, "resource_name": "my-source"},
			},
			claims:          jwt.MapClaims{"sub": "reader-1"},
			expectEventType: EventSourceRead,
			expectOutcome:   "success",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			logger := newTestLogger(&buf)

			inner := withRouteInfo(tt.routeInfo, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.innerStatus)
			}))
			handler := Middleware(enabledConfig(), logger)(inner)

			req := httptest.NewRequest(tt.method, tt.path, nil)
			if tt.claims != nil {
				ctx := auth.ContextWithClaims(req.Context(), tt.claims)
				req = req.WithContext(ctx)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assert.Equal(t, tt.innerStatus, rec.Code)

			logOutput := buf.String()
			assert.Contains(t, logOutput, "audit_event")
			assert.Contains(t, logOutput, tt.expectEventType)
			assert.Contains(t, logOutput, tt.expectOutcome)
			assert.Contains(t, logOutput, ComponentRegistryAPI)

			if tt.expectAnonymous {
				assert.Contains(t, logOutput, "anonymous")
			}
			if tt.expectSubjectSub != "" {
				assert.Contains(t, logOutput, tt.expectSubjectSub)
			}
		})
	}
}

func TestMiddleware_EmitsResourceDetail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		method       string
		path         string
		innerStatus  int
		routeInfo    *RouteInfo
		expectFields []string
	}{
		{
			name:        "source target includes resource_type and resource_name",
			method:      http.MethodPut,
			path:        "/v1/sources/my-source",
			innerStatus: http.StatusOK,
			routeInfo: &RouteInfo{
				OnCreate: EventSourceCreate,
				OnUpdate: EventSourceUpdate,
				Target:   map[string]string{"method": http.MethodPut, "path": "/v1/sources/my-source", "resource_type": ResourceTypeSource, "resource_name": "my-source"},
			},
			expectFields: []string{
				"resource_type", "source",
				"resource_name", "my-source",
			},
		},
		{
			name:        "registry target includes resource_type and resource_name",
			method:      http.MethodDelete,
			path:        "/v1/registries/my-reg",
			innerStatus: http.StatusNoContent,
			routeInfo: &RouteInfo{
				EventType: EventRegistryDelete,
				Target:    map[string]string{"method": http.MethodDelete, "path": "/v1/registries/my-reg", "resource_type": ResourceTypeRegistry, "resource_name": "my-reg"},
			},
			expectFields: []string{
				"resource_type", "registry",
				"resource_name", "my-reg",
			},
		},
		{
			name:        "entry version target includes entry_type and version",
			method:      http.MethodDelete,
			path:        "/v1/entries/server/my-server/versions/1.2.3",
			innerStatus: http.StatusNoContent,
			routeInfo: &RouteInfo{
				EventType: EventEntryDelete,
				Target: map[string]string{
					"method":        http.MethodDelete,
					"path":          "/v1/entries/server/my-server/versions/1.2.3",
					"resource_type": ResourceTypeEntry,
					"entry_type":    "server",
					"resource_name": "my-server",
					"version":       "1.2.3",
				},
			},
			expectFields: []string{
				"resource_type", "entry",
				"entry_type", "server",
				"resource_name", "my-server",
				"version", "1.2.3",
			},
		},
		{
			name:        "entry publish target includes resource_type",
			method:      http.MethodPost,
			path:        "/v1/entries",
			innerStatus: http.StatusCreated,
			routeInfo: &RouteInfo{
				EventType: EventEntryPublish,
				Target:    map[string]string{"method": http.MethodPost, "path": "/v1/entries", "resource_type": ResourceTypeEntry},
			},
			expectFields: []string{
				"resource_type", "entry",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			logger := newTestLogger(&buf)

			inner := withRouteInfo(tt.routeInfo, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.innerStatus)
			}))
			handler := Middleware(enabledConfig(), logger)(inner)

			req := httptest.NewRequest(tt.method, tt.path, nil)
			ctx := auth.ContextWithClaims(req.Context(), jwt.MapClaims{"sub": "user"})
			req = req.WithContext(ctx)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			logOutput := buf.String()
			for _, field := range tt.expectFields {
				assert.Contains(t, logOutput, field, "expected field %q in log output", field)
			}
		})
	}
}

func TestMiddleware_EmitsMetadata(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	routeInfo := &RouteInfo{
		OnCreate: EventSourceCreate,
		OnUpdate: EventSourceUpdate,
		Target:   map[string]string{"method": http.MethodPut, "path": "/v1/sources/test-src", "resource_type": ResourceTypeSource, "resource_name": "test-src"},
	}
	inner := withRouteInfo(routeInfo, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	handler := Middleware(enabledConfig(), logger)(inner)

	req := httptest.NewRequest(http.MethodPut, "/v1/sources/test-src", nil)
	ctx := auth.ContextWithClaims(req.Context(), jwt.MapClaims{"sub": "user"})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "duration_ms")
	assert.Contains(t, logOutput, "response_bytes")
}

func TestMiddleware_IncludesRequestID(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	routeInfo := &RouteInfo{
		OnCreate: EventSourceCreate,
		OnUpdate: EventSourceUpdate,
		Target:   map[string]string{"method": http.MethodPut, "path": "/v1/sources/test-src", "resource_type": ResourceTypeSource, "resource_name": "test-src"},
	}
	inner := withRouteInfo(routeInfo, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler := middleware.RequestID(Middleware(enabledConfig(), logger)(inner))

	req := httptest.NewRequest(http.MethodPut, "/v1/sources/test-src", nil)
	ctx := auth.ContextWithClaims(req.Context(), jwt.MapClaims{"sub": "user"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "audit_event")
	assert.Contains(t, logOutput, "request_id")
}

func TestMiddleware_RicherSubjects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		claims     jwt.MapClaims
		expectUser string
	}{
		{
			name:       "name claim used as user",
			claims:     jwt.MapClaims{"sub": "user-1", "name": "Alice Example"},
			expectUser: "Alice Example",
		},
		{
			name:       "preferred_username fallback",
			claims:     jwt.MapClaims{"sub": "user-2", "preferred_username": "alice"},
			expectUser: "alice",
		},
		{
			name:       "email fallback",
			claims:     jwt.MapClaims{"sub": "user-3", "email": "alice@example.com"},
			expectUser: "alice@example.com",
		},
		{
			name:       "name takes precedence over preferred_username and email",
			claims:     jwt.MapClaims{"sub": "user-4", "name": "Alice", "preferred_username": "alice", "email": "alice@example.com"},
			expectUser: "Alice",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			logger := newTestLogger(&buf)

			routeInfo := &RouteInfo{
				OnCreate: EventSourceCreate,
				OnUpdate: EventSourceUpdate,
				Target:   map[string]string{"method": http.MethodPut, "path": "/v1/sources/my-source", "resource_type": ResourceTypeSource, "resource_name": "my-source"},
			}
			inner := withRouteInfo(routeInfo, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			handler := Middleware(enabledConfig(), logger)(inner)

			req := httptest.NewRequest(http.MethodPut, "/v1/sources/my-source", nil)
			ctx := auth.ContextWithClaims(req.Context(), tt.claims)
			req = req.WithContext(ctx)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			logOutput := buf.String()
			assert.Contains(t, logOutput, tt.expectUser, "expected user display name in log output")
		})
	}
}

func TestMiddleware_RequestBodyCapture(t *testing.T) {
	t.Parallel()

	t.Run("captures body for mutating request when IncludeRequestData is true", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		logger := newTestLogger(&buf)

		cfg := &config.AuditConfig{
			Enabled:            true,
			IncludeRequestData: true,
		}

		routeInfo := &RouteInfo{
			OnCreate: EventSourceCreate,
			OnUpdate: EventSourceUpdate,
			Target:   map[string]string{"method": http.MethodPut, "path": "/v1/sources/my-source", "resource_type": ResourceTypeSource, "resource_name": "my-source"},
		}

		// The inner handler reads the body to verify it was restored.
		var innerBody []byte
		inner := withRouteInfo(routeInfo, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			innerBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusCreated)
		}))
		handler := Middleware(cfg, logger)(inner)

		body := `{"name":"my-source","type":"git"}`
		req := httptest.NewRequest(http.MethodPut, "/v1/sources/my-source", strings.NewReader(body))
		ctx := auth.ContextWithClaims(req.Context(), jwt.MapClaims{"sub": "user"})
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusCreated, rec.Code)
		// Body should be restored for the inner handler.
		assert.Equal(t, body, string(innerBody))
		// Body should appear in audit log.
		assert.Contains(t, buf.String(), "my-source")
	})

	t.Run("does not capture body for GET requests", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		logger := newTestLogger(&buf)

		cfg := &config.AuditConfig{
			Enabled:            true,
			IncludeRequestData: true,
		}

		routeInfo := &RouteInfo{
			EventType: EventSourceRead,
			Target:    map[string]string{"method": http.MethodGet, "path": "/v1/sources/my-source", "resource_type": ResourceTypeSource, "resource_name": "my-source"},
		}
		inner := withRouteInfo(routeInfo, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		handler := Middleware(cfg, logger)(inner)

		req := httptest.NewRequest(http.MethodGet, "/v1/sources/my-source", strings.NewReader(`{"ignored":true}`))
		ctx := auth.ContextWithClaims(req.Context(), jwt.MapClaims{"sub": "user"})
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		logOutput := buf.String()
		assert.Contains(t, logOutput, "audit_event")
		// GET body should not appear in the data field.
		assert.NotContains(t, logOutput, "ignored")
	})

	t.Run("does not capture body when IncludeRequestData is false", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		logger := newTestLogger(&buf)

		cfg := &config.AuditConfig{
			Enabled:            true,
			IncludeRequestData: false,
		}

		routeInfo := &RouteInfo{
			OnCreate: EventSourceCreate,
			OnUpdate: EventSourceUpdate,
			Target:   map[string]string{"method": http.MethodPut, "path": "/v1/sources/my-source", "resource_type": ResourceTypeSource, "resource_name": "my-source"},
		}
		inner := withRouteInfo(routeInfo, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
		}))
		handler := Middleware(cfg, logger)(inner)

		body := `{"name":"secret","type":"git"}`
		req := httptest.NewRequest(http.MethodPut, "/v1/sources/my-source", strings.NewReader(body))
		ctx := auth.ContextWithClaims(req.Context(), jwt.MapClaims{"sub": "user"})
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		logOutput := buf.String()
		assert.Contains(t, logOutput, "audit_event")
		assert.NotContains(t, logOutput, "secret")
	})
}

func TestMiddleware_EventTypeFiltering(t *testing.T) {
	t.Parallel()

	t.Run("whitelist allows only listed types", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		logger := newTestLogger(&buf)

		cfg := &config.AuditConfig{
			Enabled:    true,
			EventTypes: []string{EventSourceCreate},
		}

		routeInfo := &RouteInfo{
			OnCreate: EventSourceCreate,
			OnUpdate: EventSourceUpdate,
			Target:   map[string]string{"method": http.MethodPut, "path": "/v1/sources/my-source", "resource_type": ResourceTypeSource, "resource_name": "my-source"},
		}
		inner := withRouteInfo(routeInfo, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			// Returns 200, so event type resolves to source.update (not in whitelist).
			w.WriteHeader(http.StatusOK)
		}))
		handler := Middleware(cfg, logger)(inner)

		req := httptest.NewRequest(http.MethodPut, "/v1/sources/my-source", nil)
		ctx := auth.ContextWithClaims(req.Context(), jwt.MapClaims{"sub": "user"})
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.NotContains(t, buf.String(), "audit_event")
	})

	t.Run("whitelist passes matching types", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		logger := newTestLogger(&buf)

		cfg := &config.AuditConfig{
			Enabled:    true,
			EventTypes: []string{EventSourceCreate},
		}

		routeInfo := &RouteInfo{
			OnCreate: EventSourceCreate,
			OnUpdate: EventSourceUpdate,
			Target:   map[string]string{"method": http.MethodPut, "path": "/v1/sources/my-source", "resource_type": ResourceTypeSource, "resource_name": "my-source"},
		}
		inner := withRouteInfo(routeInfo, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			// Returns 201, so event type resolves to source.create (in whitelist).
			w.WriteHeader(http.StatusCreated)
		}))
		handler := Middleware(cfg, logger)(inner)

		req := httptest.NewRequest(http.MethodPut, "/v1/sources/my-source", nil)
		ctx := auth.ContextWithClaims(req.Context(), jwt.MapClaims{"sub": "user"})
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Contains(t, buf.String(), "audit_event")
	})

	t.Run("blacklist excludes listed types", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		logger := newTestLogger(&buf)

		cfg := &config.AuditConfig{
			Enabled:           true,
			ExcludeEventTypes: []string{EventSourceUpdate},
		}

		routeInfo := &RouteInfo{
			OnCreate: EventSourceCreate,
			OnUpdate: EventSourceUpdate,
			Target:   map[string]string{"method": http.MethodPut, "path": "/v1/sources/my-source", "resource_type": ResourceTypeSource, "resource_name": "my-source"},
		}
		inner := withRouteInfo(routeInfo, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		handler := Middleware(cfg, logger)(inner)

		req := httptest.NewRequest(http.MethodPut, "/v1/sources/my-source", nil)
		ctx := auth.ContextWithClaims(req.Context(), jwt.MapClaims{"sub": "user"})
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.NotContains(t, buf.String(), "audit_event")
	})

	t.Run("blacklist takes precedence over whitelist", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		logger := newTestLogger(&buf)

		cfg := &config.AuditConfig{
			Enabled:           true,
			EventTypes:        []string{EventSourceUpdate},
			ExcludeEventTypes: []string{EventSourceUpdate},
		}

		routeInfo := &RouteInfo{
			OnCreate: EventSourceCreate,
			OnUpdate: EventSourceUpdate,
			Target:   map[string]string{"method": http.MethodPut, "path": "/v1/sources/my-source", "resource_type": ResourceTypeSource, "resource_name": "my-source"},
		}
		inner := withRouteInfo(routeInfo, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		handler := Middleware(cfg, logger)(inner)

		req := httptest.NewRequest(http.MethodPut, "/v1/sources/my-source", nil)
		ctx := auth.ContextWithClaims(req.Context(), jwt.MapClaims{"sub": "user"})
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.NotContains(t, buf.String(), "audit_event")
	})
}

// --- AuthFailureMiddleware tests ---

func TestAuthFailureMiddleware_Disabled(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	tests := []struct {
		name   string
		cfg    *config.AuditConfig
		logger *Logger
	}{
		{name: "nil config", cfg: nil, logger: newTestLogger(&bytes.Buffer{})},
		{name: "disabled config", cfg: &config.AuditConfig{Enabled: false}, logger: newTestLogger(&bytes.Buffer{})},
		{name: "nil logger", cfg: enabledConfig(), logger: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := AuthFailureMiddleware(tt.cfg, tt.logger)(inner)
			req := httptest.NewRequest(http.MethodGet, "/v1/sources", nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
		})
	}
}

func TestAuthFailureMiddleware_Emits401Event(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	handler := AuthFailureMiddleware(enabledConfig(), logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/sources", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "audit_event")
	assert.Contains(t, logOutput, EventAuthUnauthenticated)
	assert.Contains(t, logOutput, "denied")
	assert.Contains(t, logOutput, "unknown")
	assert.Contains(t, logOutput, "10.0.0.1:12345")
	assert.Contains(t, logOutput, ComponentRegistryAPI)
}

func TestAuthFailureMiddleware_SkipsNon401(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
	}{
		{name: "200 OK", status: http.StatusOK},
		{name: "403 Forbidden", status: http.StatusForbidden},
		{name: "500 Internal", status: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			logger := newTestLogger(&buf)

			handler := AuthFailureMiddleware(enabledConfig(), logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))

			req := httptest.NewRequest(http.MethodGet, "/v1/sources", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assert.Empty(t, buf.String(), "non-401 status should not produce auth failure events")
		})
	}
}

func TestAuthFailureMiddleware_IncludesRequestID(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	handler := middleware.RequestID(AuthFailureMiddleware(enabledConfig(), logger)(inner))

	req := httptest.NewRequest(http.MethodGet, "/v1/sources", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	logOutput := buf.String()
	assert.Contains(t, logOutput, EventAuthUnauthenticated)
	assert.Contains(t, logOutput, "request_id")
}

func TestAuthFailureMiddleware_EventFiltering(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	cfg := &config.AuditConfig{
		Enabled:           true,
		ExcludeEventTypes: []string{EventAuthUnauthenticated},
	}

	handler := AuthFailureMiddleware(cfg, logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/sources", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Empty(t, buf.String(), "excluded event type should not produce audit events")
}

// --- Helper function tests ---

func TestIsMutating(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		method   string
		expected bool
	}{
		{name: "POST is mutating", method: http.MethodPost, expected: true},
		{name: "PUT is mutating", method: http.MethodPut, expected: true},
		{name: "DELETE is mutating", method: http.MethodDelete, expected: true},
		{name: "GET is not mutating", method: http.MethodGet, expected: false},
		{name: "HEAD is not mutating", method: http.MethodHead, expected: false},
		{name: "PATCH is not mutating", method: http.MethodPatch, expected: false},
		{name: "OPTIONS is not mutating", method: http.MethodOptions, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, isMutating(tt.method))
		})
	}
}

func TestIsEventAllowed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		eventType string
		whitelist []string
		blacklist []string
		expected  bool
	}{
		{
			name:      "empty whitelist and blacklist allows all",
			eventType: EventSourceCreate,
			expected:  true,
		},
		{
			name:      "whitelist match allows event",
			eventType: EventSourceCreate,
			whitelist: []string{EventSourceCreate, EventSourceDelete},
			expected:  true,
		},
		{
			name:      "whitelist miss blocks event",
			eventType: EventSourceUpdate,
			whitelist: []string{EventSourceCreate, EventSourceDelete},
			expected:  false,
		},
		{
			name:      "blacklist match blocks event",
			eventType: EventSourceUpdate,
			blacklist: []string{EventSourceUpdate},
			expected:  false,
		},
		{
			name:      "blacklist miss allows event",
			eventType: EventSourceCreate,
			blacklist: []string{EventSourceUpdate},
			expected:  true,
		},
		{
			name:      "blacklist takes precedence over whitelist",
			eventType: EventSourceCreate,
			whitelist: []string{EventSourceCreate},
			blacklist: []string{EventSourceCreate},
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, isEventAllowed(tt.eventType, tt.whitelist, tt.blacklist))
		})
	}
}

func TestCaptureRequestBody(t *testing.T) {
	t.Parallel()

	t.Run("captures body and restores for downstream", func(t *testing.T) {
		t.Parallel()

		body := `{"name":"test","type":"git"}`
		req := httptest.NewRequest(http.MethodPut, "/v1/sources/test", strings.NewReader(body))

		captured := captureRequestBody(req, 1024)
		assert.Equal(t, body, string(captured))

		// Verify body is still readable by downstream.
		remaining, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		assert.Equal(t, body, string(remaining))
	})

	t.Run("respects size limit", func(t *testing.T) {
		t.Parallel()

		body := strings.Repeat("a", 100)
		req := httptest.NewRequest(http.MethodPut, "/v1/sources/test", strings.NewReader(body))

		captured := captureRequestBody(req, 10)
		assert.Len(t, captured, 10)

		// Body should be fully restored for downstream (captured + remaining).
		restored, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		assert.Equal(t, body, string(restored))
	})

	t.Run("nil body returns nil", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodPut, "/v1/sources/test", nil)
		// httptest.NewRequest sets Body to http.NoBody, override to nil.
		req.Body = nil

		captured := captureRequestBody(req, 1024)
		assert.Nil(t, captured)
	})

	t.Run("empty body returns nil", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodPut, "/v1/sources/test", strings.NewReader(""))

		captured := captureRequestBody(req, 1024)
		assert.Nil(t, captured)
	})
}

func TestSubjectsFromRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		claims          jwt.MapClaims
		roles           []auth.Role
		expectSub       string
		expectUser      string
		expectAnonymous bool
		expectSuperRole bool
	}{
		{
			name:            "no claims produces anonymous",
			claims:          nil,
			expectAnonymous: true,
		},
		{
			name:      "claims with sub only",
			claims:    jwt.MapClaims{"sub": "user-1"},
			expectSub: "user-1",
		},
		{
			name:            "claims without sub produces anonymous identity",
			claims:          jwt.MapClaims{"aud": "my-api"},
			expectAnonymous: true,
		},
		{
			name:       "claims with name sets user field",
			claims:     jwt.MapClaims{"sub": "user-2", "name": "Alice Example"},
			expectSub:  "user-2",
			expectUser: "Alice Example",
		},
		{
			name:       "claims with preferred_username sets user field",
			claims:     jwt.MapClaims{"sub": "user-3", "preferred_username": "alice"},
			expectSub:  "user-3",
			expectUser: "alice",
		},
		{
			name:       "claims with email sets user field",
			claims:     jwt.MapClaims{"sub": "user-4", "email": "alice@example.com"},
			expectSub:  "user-4",
			expectUser: "alice@example.com",
		},
		{
			name:            "super admin gets role in subjects",
			claims:          jwt.MapClaims{"sub": "admin-1"},
			roles:           []auth.Role{auth.RoleSuperAdmin},
			expectSub:       "admin-1",
			expectSuperRole: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			ctx := req.Context()
			if tt.claims != nil {
				ctx = auth.ContextWithClaims(ctx, tt.claims)
			}
			if len(tt.roles) > 0 {
				ctx = auth.ContextWithRoles(ctx, tt.roles)
			}
			req = req.WithContext(ctx)

			subjects := subjectsFromRequest(req)
			require.NotNil(t, subjects)

			if tt.expectAnonymous {
				assert.Equal(t, "anonymous", subjects["identity"])
			}
			if tt.expectSub != "" {
				assert.Equal(t, tt.expectSub, subjects["sub"])
			}
			if tt.expectUser != "" {
				assert.Equal(t, tt.expectUser, subjects["user"])
			}
			if tt.expectSuperRole {
				assert.Equal(t, "super_admin", subjects["role"])
			}
		})
	}
}

func TestClaimString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		claims   map[string]any
		key      string
		expected string
	}{
		{
			name:     "existing string claim",
			claims:   map[string]any{"name": "Alice"},
			key:      "name",
			expected: "Alice",
		},
		{
			name:     "missing claim returns empty",
			claims:   map[string]any{"name": "Alice"},
			key:      "email",
			expected: "",
		},
		{
			name:     "non-string claim returns empty",
			claims:   map[string]any{"iat": 12345},
			key:      "iat",
			expected: "",
		},
		{
			name:     "nil claims map returns empty",
			claims:   nil,
			key:      "name",
			expected: "",
		},
		{
			name:     "empty string claim returns empty",
			claims:   map[string]any{"name": ""},
			key:      "name",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, claimString(tt.claims, tt.key))
		})
	}
}

func TestMiddleware_UnannotatedRouteSkips(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	// Running through the middleware without any audited* wrapper means
	// no RouteInfo is injected, so no audit event should be emitted.
	handler := Middleware(enabledConfig(), logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPut, "/v1/unknown", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Empty(t, buf.String())
}

// --- Logger tests ---

func TestLogger_NewLoggerStdout(t *testing.T) {
	t.Parallel()

	l, err := NewLogger("")
	require.NoError(t, err)
	require.NotNil(t, l)
	assert.NotNil(t, l.Slog())

	// Close should be a no-op for stdout.
	assert.NoError(t, l.Close())
}

func TestLogger_NewLoggerFile(t *testing.T) {
	t.Parallel()

	tmpFile := t.TempDir() + "/audit.log"

	l, err := NewLogger(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, l)
	assert.NotNil(t, l.Slog())

	// Write at the audit level (which the handler accepts) to verify the file is usable.
	l.Slog().Log(t.Context(), auditLevel, "test audit message")

	// Verify the file was created and has content.
	info, err := os.Stat(tmpFile)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0))

	// Close should work and not error.
	assert.NoError(t, l.Close())
}

func TestLogger_NewLoggerInvalidPath(t *testing.T) {
	t.Parallel()

	l, err := NewLogger("/nonexistent/directory/audit.log")
	assert.Error(t, err)
	assert.Nil(t, l)
	assert.Contains(t, err.Error(), "failed to open audit log file")
}

func TestLogger_Close_NilCloser(t *testing.T) {
	t.Parallel()

	// A logger created for stdout has a nil closer.
	l := &Logger{
		logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		closer: nil,
	}
	assert.NoError(t, l.Close())
}

// --- Integration-level log content verification ---

func TestMiddleware_LogOutputIsValidJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	routeInfo := &RouteInfo{
		OnCreate: EventSourceCreate,
		OnUpdate: EventSourceUpdate,
		Target:   map[string]string{"method": http.MethodPut, "path": "/v1/sources/my-source", "resource_type": ResourceTypeSource, "resource_name": "my-source"},
	}
	inner := withRouteInfo(routeInfo, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler := Middleware(enabledConfig(), logger)(inner)

	req := httptest.NewRequest(http.MethodPut, "/v1/sources/my-source", nil)
	ctx := auth.ContextWithClaims(req.Context(), jwt.MapClaims{"sub": "user-1"})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	logOutput := buf.String()
	require.NotEmpty(t, logOutput, "expected audit log output")

	// Each line should be valid JSON.
	for _, line := range strings.Split(strings.TrimSpace(logOutput), "\n") {
		if line == "" {
			continue
		}
		var parsed map[string]any
		err := json.Unmarshal([]byte(line), &parsed)
		assert.NoError(t, err, "log line should be valid JSON: %s", line)
	}
}
