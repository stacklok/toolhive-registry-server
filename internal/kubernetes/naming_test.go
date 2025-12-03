package kubernetes

import (
	"strings"
	"testing"
)

func TestGenerateServerName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		k8sNamespace  string
		k8sName       string
		expectedName  string
		expectError   bool
		errorContains string
	}{
		// Valid cases - K8s DNS label names (RFC 1123)
		// K8s already enforces: lowercase alphanumeric or '-', start with alpha, end with alphanumeric, max 63 chars
		{
			name:         "simple valid name",
			k8sNamespace: "default",
			k8sName:      "weather-service",
			expectedName: "com.toolhive.k8s.default/weather-service",
			expectError:  false,
		},
		{
			name:         "production namespace",
			k8sNamespace: "production",
			k8sName:      "api-gateway",
			expectedName: "com.toolhive.k8s.production/api-gateway",
			expectError:  false,
		},
		{
			name:         "staging with numbers",
			k8sNamespace: "staging",
			k8sName:      "service-v2",
			expectedName: "com.toolhive.k8s.staging/service-v2",
			expectError:  false,
		},
		{
			name:         "complex hyphenated names",
			k8sNamespace: "dev-team-1",
			k8sName:      "my-awesome-service",
			expectedName: "com.toolhive.k8s.dev-team-1/my-awesome-service",
			expectError:  false,
		},
		{
			name:         "short single letter components",
			k8sNamespace: "a",
			k8sName:      "b",
			expectedName: "com.toolhive.k8s.a/b",
			expectError:  false,
		},
		{
			name:         "numbers in middle",
			k8sNamespace: "team123",
			k8sName:      "svc456",
			expectedName: "com.toolhive.k8s.team123/svc456",
			expectError:  false,
		},
		{
			name:         "kube-system namespace",
			k8sNamespace: "kube-system",
			k8sName:      "kube-dns",
			expectedName: "com.toolhive.k8s.kube-system/kube-dns",
			expectError:  false,
		},
		{
			name:         "helm release name",
			k8sNamespace: "default",
			k8sName:      "my-release-postgresql",
			expectedName: "com.toolhive.k8s.default/my-release-postgresql",
			expectError:  false,
		},
		{
			name:         "operator generated name",
			k8sNamespace: "operators",
			k8sName:      "prometheus-operator-12345",
			expectedName: "com.toolhive.k8s.operators/prometheus-operator-12345",
			expectError:  false,
		},

		// Error cases - empty inputs
		{
			name:          "empty namespace",
			k8sNamespace:  "",
			k8sName:       "service",
			expectError:   true,
			errorContains: "namespace cannot be empty",
		},
		{
			name:          "empty name",
			k8sNamespace:  "default",
			k8sName:       "",
			expectError:   true,
			errorContains: "name cannot be empty",
		},
		{
			name:          "both empty",
			k8sNamespace:  "",
			k8sName:       "",
			expectError:   true,
			errorContains: "namespace cannot be empty",
		},

		// Error case - exceeding length limit (200 chars total)
		{
			name:         "very long name exceeding 200 chars",
			k8sNamespace: "namespace-with-a-really-long-name-that-keeps-going",
			k8sName: "service-with-an-extremely-long-name-that-when-combined-with-the-prefix-and-namespace-" +
				"will-definitely-exceed-the-two-hundred-character-limit-that-we-have-set-for-server-names-in-the-database",
			expectError:   true,
			errorContains: "exceeds maximum length",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := GenerateServerName(tt.k8sNamespace, tt.k8sName)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, got nil")
					return
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing %q, got %q", tt.errorContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if result != tt.expectedName {
				t.Errorf("Expected %q, got %q", tt.expectedName, result)
			}
		})
	}
}
