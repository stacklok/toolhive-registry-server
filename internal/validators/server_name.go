// Package validators provides validation functions for MCP Registry Server entities.
package validators

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	minServerNameLength = 3
	maxServerNameLength = 200
)

var (
	// Namespace pattern: must start and end with alphanumeric, can contain dots and hyphens in the middle
	namespacePattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9.-]*[a-zA-Z0-9])?$`)

	// Name pattern: must start and end with alphanumeric, can contain dots, underscores, and hyphens in the middle
	namePattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?$`)
)

// ValidateServerName validates a server name according to the MCP Registry specification.
// The name must be in reverse-DNS format: namespace/name
// Returns the validated name (trimmed) and an error if validation fails.
//
// Format requirements:
// - Must contain exactly one '/' separator
// - Namespace (before /): [a-zA-Z0-9][a-zA-Z0-9.-]*[a-zA-Z0-9]
// - Name (after /): [a-zA-Z0-9][a-zA-Z0-9._-]*[a-zA-Z0-9]
// - Total length: 3-200 characters
//
// Examples of valid names:
//   - com.example/server
//   - org.stacklok.toolhive/my-server
//   - io.github.user/test_server
//
// Examples of invalid names:
//   - my-server (missing slash)
//   - com.example//server (multiple slashes)
//   - .example/server (namespace starts with dot)
//   - com.example/server- (name ends with dash)
func ValidateServerName(name string) (string, error) {
	// Trim whitespace
	name = strings.TrimSpace(name)

	// Check empty
	if name == "" {
		return "", fmt.Errorf("server name cannot be empty")
	}

	// Check for exactly one slash (before length check for better error messages)
	slashCount := strings.Count(name, "/")
	if slashCount == 0 {
		return "", fmt.Errorf("server name must be in format 'dns-namespace/name' (e.g., 'com.example.api/server')")
	}
	if slashCount > 1 {
		return "", fmt.Errorf("server name must contain exactly one '/' separator")
	}

	// Split into namespace and name parts
	parts := strings.SplitN(name, "/", 2)
	namespace := parts[0]
	namePart := parts[1]

	// Validate namespace
	if namespace == "" {
		return "", fmt.Errorf("namespace part cannot be empty")
	}

	// Validate name part
	if namePart == "" {
		return "", fmt.Errorf("name part cannot be empty")
	}

	// Check length (after splitting for better error messages)
	if len(name) < minServerNameLength {
		return "", fmt.Errorf("server name must be at least %d characters long", minServerNameLength)
	}
	if len(name) > maxServerNameLength {
		return "", fmt.Errorf("server name exceeds maximum length of %d characters", maxServerNameLength)
	}
	if !namespacePattern.MatchString(namespace) {
		return "", fmt.Errorf(
			"namespace '%s' is invalid. Namespace must start and end with alphanumeric characters, "+
				"and may contain dots and hyphens in the middle",
			namespace,
		)
	}

	// Validate name part pattern
	if !namePattern.MatchString(namePart) {
		return "", fmt.Errorf(
			"name '%s' is invalid. Name must start and end with alphanumeric characters, "+
				"and may contain dots, underscores, and hyphens in the middle",
			namePart,
		)
	}

	return name, nil
}

// IsValidServerName checks if a server name is valid according to the MCP Registry specification.
// Returns true if valid, false otherwise.
// This is a convenience wrapper around ValidateServerName for boolean checks.
func IsValidServerName(name string) bool {
	_, err := ValidateServerName(name)
	return err == nil
}
