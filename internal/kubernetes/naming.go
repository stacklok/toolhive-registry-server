package kubernetes

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/stacklok/toolhive-registry-server/internal/validators"
)

const (
	// K8s server name prefix for auto-discovered servers
	k8sServerNamePrefix = "com.toolhive.k8s"
)

var (
	// Pattern for characters invalid in namespace (namespace doesn't allow underscores)
	invalidNamespaceChars = regexp.MustCompile(`[^a-zA-Z0-9.-]`)
	// Pattern for characters invalid in name part (name allows underscores)
	invalidNameChars = regexp.MustCompile(`[^a-zA-Z0-9._-]`)
)

// GenerateServerName generates a reverse-DNS formatted server name from K8s resource information.
// The format is: com.toolhive.k8s.<namespace>/<service-name>
//
// The function sanitizes K8s names to ensure reverse-DNS compatibility:
//   - Converts to lowercase
//   - Replaces invalid characters with dashes
//   - Removes leading/trailing special characters
//
// Returns an error if:
//   - namespace or name is empty
//   - the generated name exceeds 200 characters
//   - the generated name fails reverse-DNS validation
//
// Examples:
//   - GenerateServerName("default", "weather-service") -> "com.toolhive.k8s.default/weather-service"
//   - GenerateServerName("prod", "MyServer") -> "com.toolhive.k8s.prod/myserver"
//   - GenerateServerName("dev", "my_server") -> "com.toolhive.k8s.dev/my-server"
func GenerateServerName(k8sNamespace, k8sName string) (string, error) {
	// Validate inputs
	if k8sNamespace == "" {
		return "", fmt.Errorf("kubernetes namespace cannot be empty")
	}
	if k8sName == "" {
		return "", fmt.Errorf("kubernetes name cannot be empty")
	}

	// Sanitize namespace and name for reverse-DNS compatibility
	sanitizedNamespace := sanitizeNamespace(k8sNamespace)
	sanitizedName := sanitizeName(k8sName)

	// Build the server name
	serverName := fmt.Sprintf("%s.%s/%s", k8sServerNamePrefix, sanitizedNamespace, sanitizedName)

	// Validate the generated name using the validators package
	validatedName, err := validators.ValidateServerName(serverName)
	if err != nil {
		return "", fmt.Errorf("generated server name validation failed: %w", err)
	}

	return validatedName, nil
}

// sanitizeNamespace converts a Kubernetes namespace to be compatible with reverse-DNS namespace format.
// Namespace rules (no underscores allowed):
//   - Convert to lowercase
//   - Replace invalid characters (including underscores) with hyphens
//   - Remove leading/trailing hyphens, dots, and underscores
func sanitizeNamespace(namespace string) string {
	// Convert to lowercase
	namespace = strings.ToLower(namespace)

	// Replace invalid characters with hyphens (namespace doesn't allow underscores)
	namespace = invalidNamespaceChars.ReplaceAllString(namespace, "-")

	// Remove leading and trailing special characters
	namespace = strings.Trim(namespace, "-._")

	return namespace
}

// sanitizeName converts a Kubernetes name to be compatible with reverse-DNS name format.
// Name rules (underscores are allowed):
//   - Convert to lowercase
//   - Replace invalid characters with hyphens (but keep underscores)
//   - Remove leading/trailing hyphens, dots, and underscores
func sanitizeName(name string) string {
	// Convert to lowercase
	name = strings.ToLower(name)

	// Replace invalid characters with hyphens (name part allows underscores)
	name = invalidNameChars.ReplaceAllString(name, "-")

	// Remove leading and trailing special characters
	name = strings.Trim(name, "-._")

	return name
}
