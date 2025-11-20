// Package auth provides authentication middleware for the registry API server.
package auth

import "errors"

// Domain errors for authentication
var (
	// ErrAllProvidersFailed indicates all providers failed during sequential fallback
	ErrAllProvidersFailed = errors.New("all providers failed to validate token")
)

// ValidationResult contains the outcome of token validation
type ValidationResult struct {
	// Provider is the name of the provider that validated the token
	Provider string

	// Error is set if validation failed
	Error error

	// Errors contains all errors from sequential fallback (for debugging)
	Errors []ProviderError
}

// ProviderError pairs a provider name with its validation error
type ProviderError struct {
	Provider string
	Error    error
}
