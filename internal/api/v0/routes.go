// Package v0 provides the REST API handlers for MCP Registry access.
package v0

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/stacklok/toolhive/pkg/logger"
	"github.com/stacklok/toolhive/pkg/versions"
	"gopkg.in/yaml.v3"

	"github.com/stacklok/toolhive-registry-server/cmd/thv-registry-api/docs"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

var (
	// cachedOpenAPIYAML stores the cached YAML representation of the OpenAPI spec
	cachedOpenAPIYAML []byte
)

func init() {
	// Initialize the OpenAPI YAML at package load time to prevent race conditions
	// Parse the JSON OpenAPI spec
	var openAPISpec map[string]any
	if err := json.Unmarshal([]byte(docs.SwaggerInfo.ReadDoc()), &openAPISpec); err != nil {
		logger.Errorf("Failed to parse OpenAPI specification during initialization: %v", err)
		return
	}

	// Convert to YAML
	yamlData, err := yaml.Marshal(openAPISpec)
	if err != nil {
		logger.Errorf("Failed to convert OpenAPI specification to YAML during initialization: %v", err)
		return
	}

	cachedOpenAPIYAML = yamlData
}

// RegistryInfoResponse represents the registry information response
// Deprecated: Use API v0.1 instead
type RegistryInfoResponse struct {
	Version      string `json:"version"`
	LastUpdated  string `json:"last_updated"`
	Source       string `json:"source"`
	TotalServers int    `json:"total_servers"`
}

// ErrorResponse represents a standardized error response
// Deprecated: Use API v0.1 instead
type ErrorResponse struct {
	Error string `json:"error"`
}

// Routes defines the routes for the registry API with dependency injection
// Deprecated: Use API v0.1 instead
type Routes struct {
	service service.RegistryService
}

// NewRoutes creates a new Routes instance with the provided service
// Deprecated: Use API v0.1 instead
func NewRoutes(svc service.RegistryService) *Routes {
	return &Routes{
		service: svc,
	}
}

// Router creates a new router for the registry API
// Deprecated: Use API v0.1 instead
func Router(svc service.RegistryService) http.Handler {
	routes := NewRoutes(svc)

	r := chi.NewRouter()

	// Registry metadata
	r.Get("/info", routes.getRegistryInfo)

	// OpenAPI specification
	r.Get("/openapi.yaml", serveOpenAPIYAML)

	return r
}

// getRegistryInfo handles GET /api/v0/registry/info
//
// @Summary		Get registry information
// @Description	Get registry metadata including version, last updated time, and total servers
// @Tags			registry
// @Accept			json
// @Produce		json
// @Param			format	query		string	false	"Response format"	Enums(toolhive,upstream)	default(toolhive)
// @Success		200		{object}	RegistryInfoResponse
// @Failure		400		{object}	ErrorResponse
// @Failure		501		{object}	ErrorResponse
// @Router			/api/v0/registry/info [get]
// @Deprecated
func (rr *Routes) getRegistryInfo(w http.ResponseWriter, r *http.Request) {
	reg, source, err := rr.service.GetRegistry(r.Context())
	if err != nil {
		logger.Errorf("Failed to get registry: %v", err)
		rr.writeErrorResponse(w, "Failed to get registry information", http.StatusInternalServerError)
		return
	}

	info := RegistryInfoResponse{
		Version:      reg.Version,
		LastUpdated:  reg.LastUpdated,
		Source:       source,
		TotalServers: len(reg.Servers),
	}

	rr.writeJSONResponse(w, info)
}

// HealthRouter creates a router for health check endpoints
func HealthRouter(svc service.RegistryService) http.Handler {
	r := chi.NewRouter()

	r.Get("/health", healthHandler)
	r.Get("/readiness", readinessHandler(svc))
	r.Get("/version", versionHandler)

	return r
}

// healthHandler handles health check requests
//
// @Summary		Health check
// @Description	Check if the registry API is healthy
// @Tags			system
// @Produce		json
// @Success		200	{object}	map[string]string
// @Router			/health [get]
// @Deprecated
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"healthy"}`))
}

// readinessHandler handles readiness check requests
//
// @Summary		Readiness check
// @Description	Check if the registry API is ready to serve requests
// @Tags			system
// @Produce		json
// @Success		200	{object}	map[string]string
// @Failure		503	{object}	ErrorResponse
// @Router			/readiness [get]
// @Deprecated
func readinessHandler(svc service.RegistryService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := svc.CheckReadiness(r.Context()); err != nil {
			errorResp := ErrorResponse{
				Error: "RegistryService not ready: " + err.Error(),
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			if encodeErr := json.NewEncoder(w).Encode(errorResp); encodeErr != nil {
				logger.Errorf("Failed to encode readiness error response: %v", encodeErr)
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
// @Tags			system
// @Produce		json
// @Success		200	{object}	map[string]string
// @Router			/version [get]
// @Deprecated
func versionHandler(w http.ResponseWriter, _ *http.Request) {
	info := versions.GetVersionInfo()

	response := map[string]string{
		"version":    info.Version,
		"commit":     info.Commit,
		"build_date": info.BuildDate,
		"go_version": info.GoVersion,
		"platform":   info.Platform,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Errorf("Failed to encode version info: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
}

// writeJSONResponse writes a JSON response with the given data
func (*Routes) writeJSONResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		logger.Errorf("Failed to encode JSON response: %v", err)
	}
}

// writeErrorResponse writes a standardized error response
func (*Routes) writeErrorResponse(w http.ResponseWriter, message string, statusCode int) {
	errorResp := ErrorResponse{
		Error: message,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(errorResp); err != nil {
		logger.Errorf("Failed to encode error response: %v", err)
	}
}

// serveOpenAPIYAML serves the OpenAPI specification in YAML format
//
// @Summary		Get OpenAPI specification
// @Description	Returns the OpenAPI specification for the registry API in YAML format
// @Tags			system
// @Produce		application/x-yaml
// @Success		200	{string}	string	"OpenAPI specification in YAML format"
// @Router			/api/v0/registry/openapi.yaml [get]
// @Deprecated
func serveOpenAPIYAML(w http.ResponseWriter, _ *http.Request) {
	// Check if initialization failed (cachedOpenAPIYAML would be empty)
	if len(cachedOpenAPIYAML) == 0 {
		http.Error(w, "OpenAPI specification not available", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/x-yaml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(cachedOpenAPIYAML)
}
