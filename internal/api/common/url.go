// Package common provides shared HTTP utility functions for API handlers.
package common

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
)

// GetAndValidateURLParam extracts, decodes, and validates a URL parameter from the request.
// Returns the decoded value or an error if invalid.
// Validation rules:
// - Must not be empty after trimming whitespace
// - Must not contain any whitespace characters
func GetAndValidateURLParam(r *http.Request, paramName string) (string, error) {
	// Extract from chi router
	encodedValue := chi.URLParam(r, paramName)

	// Decode
	decoded, err := url.PathUnescape(encodedValue)
	if err != nil {
		return "", fmt.Errorf("invalid URL encoding in %s", paramName)
	}

	// Validate - check if empty
	if strings.TrimSpace(decoded) == "" {
		return "", fmt.Errorf("%s cannot be empty", paramName)
	}

	// Validate - check for whitespace
	if strings.ContainsAny(decoded, " \t\n\r") {
		return "", fmt.Errorf("%s cannot contain whitespace", paramName)
	}

	return decoded, nil
}
