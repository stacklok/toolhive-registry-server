// Package api provides common API types and responses.
package api

// HealthResponse represents the health check response
type HealthResponse struct {
	Status string `json:"status" example:"healthy"`
}

// ReadinessResponse represents the readiness check response
type ReadinessResponse struct {
	Status string `json:"status" example:"ready"`
}

// VersionResponse represents the version information response
type VersionResponse struct {
	Version   string `json:"version" example:"v0.1.0"`
	Commit    string `json:"commit" example:"abc123def"`
	BuildDate string `json:"build_date" example:"2025-01-15T10:30:00Z"`
	GoVersion string `json:"go_version" example:"go1.21.5"`
	Platform  string `json:"platform" example:"linux/amd64"`
}
