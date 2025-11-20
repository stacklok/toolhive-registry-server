// Package api provides the REST API server for MCP Registry access.
package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/stacklok/toolhive/pkg/logger"

	extensionv0 "github.com/stacklok/toolhive-registry-server/internal/api/extension/v0"
	v01 "github.com/stacklok/toolhive-registry-server/internal/api/registry/v01"
	v0 "github.com/stacklok/toolhive-registry-server/internal/api/v0"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// ServerOption configures the registry API server
type ServerOption func(*serverConfig)

// serverConfig holds the server configuration
type serverConfig struct {
	middlewares []func(http.Handler) http.Handler
}

// WithMiddlewares adds middleware to the server
func WithMiddlewares(mw ...func(http.Handler) http.Handler) ServerOption {
	return func(cfg *serverConfig) {
		cfg.middlewares = append(cfg.middlewares, mw...)
	}
}

// NewServer creates and configures the HTTP router with the given service and options
func NewServer(svc service.RegistryService, opts ...ServerOption) *chi.Mux {
	// Initialize configuration with defaults
	cfg := &serverConfig{
		middlewares: []func(http.Handler) http.Handler{},
	}

	// Apply options
	for _, opt := range opts {
		opt(cfg)
	}

	r := chi.NewRouter()

	// Apply middleware
	for _, mw := range cfg.middlewares {
		r.Use(mw)
	}

	// Mount health check routes directly at root
	r.Mount("/", v0.HealthRouter(svc))

	// Mount OpenAPI endpoint
	r.Get("/openapi.json", openAPIHandler)

	// Mount MCP Registry API v0 compatible routes
	r.Mount("/registry", v01.Router(svc))
	r.Mount("/extension/v0", extensionv0.Router(svc))

	return r
}

// LoggingMiddleware logs HTTP requests
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		logger.Debugf("HTTP %s %s %d %s %s",
			r.Method,
			r.URL.Path,
			ww.Status(),
			time.Since(start),
			middleware.GetReqID(r.Context()),
		)
	})
}

// openAPIHandler handles OpenAPI specification requests
func openAPIHandler(w http.ResponseWriter, _ *http.Request) {
	// TODO: Implement OpenAPI spec serving
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	_, _ = w.Write([]byte(`{"error":"OpenAPI specification not yet implemented"}`))
}
