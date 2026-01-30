// Package telemetry provides OpenTelemetry instrumentation for the registry server.
// It supports configurable tracing and metrics with OTLP exporters.
package telemetry

import (
	"errors"
	"fmt"
)

const (
	// DefaultServiceName is the default service name for telemetry
	DefaultServiceName = "thv-registry-api"

	// DefaultEndpoint is the default OTLP endpoint for telemetry
	DefaultEndpoint = "localhost:4318"

	// DefaultSampling is the default trace sampling rate (5%)
	// This provides a reasonable balance between observability and overhead
	DefaultSampling = 0.05
)

// Config represents the root telemetry configuration
type Config struct {
	// Enabled controls whether telemetry is enabled globally
	// When false, no telemetry providers are initialized
	Enabled bool `yaml:"enabled"`

	// ServiceName is the name of the service for telemetry identification
	// Defaults to "thv-registry-api" if not specified
	ServiceName string `yaml:"serviceName,omitempty"`

	// ServiceVersion is the version of the service for telemetry identification
	// Defaults to the application version if not specified
	ServiceVersion string `yaml:"serviceVersion,omitempty"`

	// Endpoint is the OTLP collector endpoint for telemetry
	// Defaults to "localhost:4318" if not specified
	// Format: "host:port" for HTTP (uses /v1/traces and /v1/metrics paths automatically)
	Endpoint string `yaml:"endpoint,omitempty"`

	// Insecure allows HTTP connections instead of HTTPS
	// Should only be true for development/testing environments
	Insecure bool `yaml:"insecure,omitempty"`

	// Tracing contains tracing-specific configuration
	Tracing *TracingConfig `yaml:"tracing,omitempty"`

	// Metrics contains metrics-specific configuration
	Metrics *MetricsConfig `yaml:"metrics,omitempty"`
}

// TracingConfig defines tracing-specific configuration
type TracingConfig struct {
	// Enabled controls whether tracing is enabled
	// When false, tracing is disabled even if telemetry is enabled globally
	Enabled bool `yaml:"enabled"`

	// Sampling controls the trace sampling rate (0.0 to 1.0, exclusive of 0.0)
	// 1.0 means sample all traces, 0.5 means sample 50%, etc.
	// Defaults to DefaultSampling if not specified (nil)
	// Using a pointer allows distinguishing between "not set" and "explicitly set to 0"
	// Setting sampling to 0 when tracing is enabled is an error (use Enabled: false instead)
	Sampling *float64 `yaml:"sampling,omitempty"`
}

// MetricsConfig defines metrics-specific configuration
type MetricsConfig struct {
	// Enabled controls whether metrics collection is enabled
	// When false, metrics are disabled even if telemetry is enabled globally
	Enabled bool `yaml:"enabled"`
}

// GetServiceName returns the service name, using default if not specified
func (c *Config) GetServiceName() string {
	if c.ServiceName == "" {
		return DefaultServiceName
	}
	return c.ServiceName
}

// GetServiceVersion returns the service version, using "unknown" if not specified
func (c *Config) GetServiceVersion() string {
	if c.ServiceVersion == "" {
		return "unknown"
	}
	return c.ServiceVersion
}

// GetEndpoint returns the endpoint, using default if not specified
func (c *Config) GetEndpoint() string {
	if c.Endpoint == "" {
		return DefaultEndpoint
	}
	return c.Endpoint
}

// GetInsecure returns the insecure flag
func (c *Config) GetInsecure() bool {
	return c.Insecure
}

// GetSampling returns the sampling ratio.
// If Sampling is nil (not specified), it returns DefaultSampling.
// Validation should be performed before calling this method.
func (c *TracingConfig) GetSampling() float64 {
	if c.Sampling == nil {
		return DefaultSampling
	}
	return *c.Sampling
}

// Validate validates the telemetry configuration
func (c *Config) Validate() error {
	if c == nil {
		return nil // nil config is valid (telemetry disabled)
	}

	if !c.Enabled {
		return nil // disabled telemetry needs no further validation
	}

	var errs []error

	if c.Tracing != nil {
		if err := c.Tracing.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("tracing: %w", err))
		}
	}

	if c.Metrics != nil {
		if err := c.Metrics.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("metrics: %w", err))
		}
	}

	return errors.Join(errs...)
}

// Validate validates the tracing configuration
func (c *TracingConfig) Validate() error {
	if c == nil || !c.Enabled {
		return nil
	}

	// Only validate sampling if explicitly set
	if c.Sampling != nil {
		sampling := *c.Sampling
		if sampling <= 0 || sampling > 1.0 {
			return fmt.Errorf("sampling must be greater than 0.0 and at most 1.0, got %f (use enabled: false to disable tracing)", sampling)
		}
	}

	return nil
}

// Validate validates the metrics configuration
func (c *MetricsConfig) Validate() error {
	if c == nil || !c.Enabled {
		return nil
	}

	// No additional validation needed for OTLP-only configuration
	return nil
}
