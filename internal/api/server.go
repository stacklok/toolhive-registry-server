// Package api provides the REST API server for MCP Registry access.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/stacklok/toolhive/pkg/versions"
	"github.com/swaggo/swag/v2"

	// Import generated docs package to register OpenAPI spec via init()
	_ "github.com/stacklok/toolhive-registry-server/docs/thv-registry-api"
	extensionv0 "github.com/stacklok/toolhive-registry-server/internal/api/extension/v0"
	v01 "github.com/stacklok/toolhive-registry-server/internal/api/registry/v01"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// ServerOption configures the registry API server
type ServerOption func(*serverConfig)

// serverConfig holds the server configuration
type serverConfig struct {
	middlewares     []func(http.Handler) http.Handler
	authInfoHandler http.Handler
}

// WithMiddlewares adds middleware to the server
func WithMiddlewares(mw ...func(http.Handler) http.Handler) ServerOption {
	return func(cfg *serverConfig) {
		cfg.middlewares = append(cfg.middlewares, mw...)
	}
}

// WithAuthInfoHandler sets the auth info handler to be mounted at /.well-known/oauth-protected-resource
func WithAuthInfoHandler(handler http.Handler) ServerOption {
	return func(cfg *serverConfig) {
		cfg.authInfoHandler = handler
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

	// Mount operational endpoints at root
	r.Get("/health", healthHandler)
	r.Get("/readiness", readinessHandler(svc))
	r.Get("/version", versionHandler)

	// Mount OpenAPI endpoint
	r.Get("/openapi.json", openAPIHandler)

	// Mount auth info handler at well-known endpoint (if configured)
	if cfg.authInfoHandler != nil {
		r.Handle("/.well-known/oauth-protected-resource", cfg.authInfoHandler)
	}

	// Mount MCP Registry API v0.1 routes
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

		// Determine log level based on status code
		status := ww.Status()
		duration := time.Since(start)
		logLevel := slog.LevelInfo
		if status >= 500 {
			logLevel = slog.LevelError
		} else if status >= 400 {
			logLevel = slog.LevelWarn
		}

		slog.Log(r.Context(), logLevel, "HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"duration_ms", duration.Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()),
			"remote_addr", r.RemoteAddr,
		)
	})
}

// openAPIHandler handles OpenAPI specification requests
//
// @Summary		OpenAPI specification
// @Description	Get the OpenAPI 3.1.0 specification for this API
// @Tags		system
// @Produce		json
// @Success		200	{object}	object	"OpenAPI 3.1.0 specification"
// @Failure		500	{object}	map[string]string
// @Router		/openapi.json [get]
func openAPIHandler(w http.ResponseWriter, _ *http.Request) {
	doc, err := swag.ReadDoc("swagger")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		errorResp := map[string]string{
			"error": "Failed to read OpenAPI specification: " + err.Error(),
		}
		if encodeErr := json.NewEncoder(w).Encode(errorResp); encodeErr != nil {
			slog.Error("Failed to encode OpenAPI error response", "error", encodeErr)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(doc))
}

// healthHandler handles health check requests
//
// @Summary		Health check
// @Description	Check if the registry API is healthy
// @Tags		system
// @Produce		json
// @Success		200	{object}	map[string]string
// @Router		/health [get]
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"healthy"}`))
}

// readinessHandler handles readiness check requests
//
// @Summary		Readiness check
// @Description	Check if the registry API is ready to serve requests
// @Tags		system
// @Produce		json
// @Success		200	{object}	map[string]string
// @Failure		503	{object}	map[string]string
// @Router		/readiness [get]
func readinessHandler(svc service.RegistryService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := svc.CheckReadiness(r.Context()); err != nil {
			slog.Warn("Readiness check failed",
				"error", err,
				"remote_addr", r.RemoteAddr)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			errorResp := map[string]string{
				"error": "RegistryService not ready: " + err.Error(),
			}
			if encodeErr := json.NewEncoder(w).Encode(errorResp); encodeErr != nil {
				slog.Error("Failed to encode readiness error response", "error", encodeErr)
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	}
}

// versionHandler handles version information requests
//
// @Summary		Version information
// @Description	Get version information about the registry API
// @Tags		system
// @Produce		json
// @Success		200	{object}	map[string]string
// @Router		/version [get]
func versionHandler(w http.ResponseWriter, _ *http.Request) {
	info := versions.GetVersionInfo()

	response := map[string]string{
		"version":    info.Version,
		"commit":     info.Commit,
		"build_date": info.BuildDate,
		"go_version": info.GoVersion,
		"platform":   info.Platform,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("Failed to encode version info", "error", err)
	}
}
