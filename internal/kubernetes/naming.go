package kubernetes

import (
	"fmt"
)

const (
	// k8sServerNamePrefix is the prefix for auto-discovered K8s servers
	k8sServerNamePrefix = "com.toolhive.k8s"

	// maxServerNameLength is the maximum allowed length for a server name
	maxServerNameLength = 200
)

// GenerateServerName generates a reverse-DNS formatted server name from K8s resource information.
// The format is: com.toolhive.k8s.<namespace>/<service-name>
//
// Note: Kubernetes already enforces DNS label name rules (RFC 1123) for namespaces and names:
//   - Only lowercase alphanumeric characters or '-'
//   - Start with an alphabetic character
//   - End with an alphanumeric character
//   - Max 63 characters each
//
// Therefore, no sanitization is needed - we simply concatenate the values.
//
// Returns an error if:
//   - namespace or name is empty
//   - the generated name exceeds 200 characters (database/spec limit)
//
// Examples:
//   - GenerateServerName("default", "weather-service") -> "com.toolhive.k8s.default/weather-service"
//   - GenerateServerName("production", "api-gateway") -> "com.toolhive.k8s.production/api-gateway"
func GenerateServerName(k8sNamespace, k8sName string) (string, error) {
	if k8sNamespace == "" {
		return "", fmt.Errorf("kubernetes namespace cannot be empty")
	}
	if k8sName == "" {
		return "", fmt.Errorf("kubernetes name cannot be empty")
	}

	// K8s already enforces DNS label rules, so we can directly concatenate
	serverName := fmt.Sprintf("%s.%s/%s", k8sServerNamePrefix, k8sNamespace, k8sName)

	// Check length constraint (200 char limit from database/spec)
	if len(serverName) > maxServerNameLength {
		return "", fmt.Errorf("generated server name exceeds maximum length of %d characters: %s",
			maxServerNameLength, serverName)
	}

	return serverName, nil
}
