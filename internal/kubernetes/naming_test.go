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
		// Valid cases - standard K8s names
		{
			name:         "simple valid name",
			k8sNamespace: "default",
			k8sName:      "weather-service",
			expectedName: "com.toolhive.k8s.default/weather-service",
			expectError:  false,
		},
		{
			name:         "production namespace",
			k8sNamespace: "prod",
			k8sName:      "api-server",
			expectedName: "com.toolhive.k8s.prod/api-server",
			expectError:  false,
		},
		{
			name:         "namespace with hyphens",
			k8sNamespace: "dev-team-1",
			k8sName:      "test-server",
			expectedName: "com.toolhive.k8s.dev-team-1/test-server",
			expectError:  false,
		},

		// Sanitization cases - uppercase
		{
			name:         "uppercase namespace",
			k8sNamespace: "PROD",
			k8sName:      "server",
			expectedName: "com.toolhive.k8s.prod/server",
			expectError:  false,
		},
		{
			name:         "uppercase name",
			k8sNamespace: "default",
			k8sName:      "MyServer",
			expectedName: "com.toolhive.k8s.default/myserver",
			expectError:  false,
		},
		{
			name:         "mixed case",
			k8sNamespace: "Dev",
			k8sName:      "WeatherAPI",
			expectedName: "com.toolhive.k8s.dev/weatherapi",
			expectError:  false,
		},

		// Sanitization cases - underscores
		{
			name:         "underscore in namespace - converted to hyphen",
			k8sNamespace: "team_1",
			k8sName:      "server",
			expectedName: "com.toolhive.k8s.team-1/server",
			expectError:  false,
		},
		{
			name:         "underscore in name - preserved",
			k8sNamespace: "default",
			k8sName:      "my_server",
			expectedName: "com.toolhive.k8s.default/my_server",
			expectError:  false,
		},
		{
			name:         "multiple underscores",
			k8sNamespace: "team_dev_1",
			k8sName:      "api_server_v2",
			expectedName: "com.toolhive.k8s.team-dev-1/api_server_v2",
			expectError:  false,
		},

		// Sanitization cases - special characters
		{
			name:         "special characters in namespace",
			k8sNamespace: "team@prod",
			k8sName:      "server",
			expectedName: "com.toolhive.k8s.team-prod/server",
			expectError:  false,
		},
		{
			name:         "special characters in name",
			k8sNamespace: "default",
			k8sName:      "server#1",
			expectedName: "com.toolhive.k8s.default/server-1",
			expectError:  false,
		},

		// Sanitization cases - leading/trailing special chars
		{
			name:         "leading hyphen in namespace",
			k8sNamespace: "-team",
			k8sName:      "server",
			expectedName: "com.toolhive.k8s.team/server",
			expectError:  false,
		},
		{
			name:         "trailing hyphen in name",
			k8sNamespace: "default",
			k8sName:      "server-",
			expectedName: "com.toolhive.k8s.default/server",
			expectError:  false,
		},
		{
			name:         "leading and trailing underscores",
			k8sNamespace: "_team_",
			k8sName:      "_server_",
			expectedName: "com.toolhive.k8s.team/server",
			expectError:  false,
		},
		{
			name:         "leading dot in namespace",
			k8sNamespace: ".team",
			k8sName:      "server",
			expectedName: "com.toolhive.k8s.team/server",
			expectError:  false,
		},

		// Sanitization cases - multiple issues
		{
			name:         "complex sanitization",
			k8sNamespace: "_Team@123_",
			k8sName:      "My_Server#V2",
			expectedName: "com.toolhive.k8s.team-123/my_server-v2",
			expectError:  false,
		},

		// Error cases - empty inputs
		{
			name:          "empty namespace",
			k8sNamespace:  "",
			k8sName:       "server",
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

		// Error cases - sanitization results in invalid names
		{
			name:          "only special characters in namespace",
			k8sNamespace:  "---",
			k8sName:       "server",
			expectError:   true,
			errorContains: "validation failed",
		},
		{
			name:          "only special characters in name",
			k8sNamespace:  "default",
			k8sName:       "___",
			expectError:   true,
			errorContains: "validation failed",
		},

		// Error cases - length constraints
		{
			name:          "very long namespace",
			k8sNamespace:  strings.Repeat("a", 180),
			k8sName:       "server",
			expectError:   true,
			errorContains: "validation failed",
		},
		{
			name:          "very long name",
			k8sNamespace:  "default",
			k8sName:       strings.Repeat("s", 180),
			expectError:   true,
			errorContains: "validation failed",
		},
		{
			name:          "combined length too long",
			k8sNamespace:  strings.Repeat("a", 100),
			k8sName:       strings.Repeat("b", 100),
			expectError:   true,
			errorContains: "exceeds maximum length",
		},

		// Real-world K8s names
		{
			name:         "real k8s service name",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := GenerateServerName(tt.k8sNamespace, tt.k8sName)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, got nil")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing %q, got %q", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
				if result != tt.expectedName {
					t.Errorf("Expected name %q, got %q", tt.expectedName, result)
				}
			}
		})
	}
}

func TestSanitizeNamespace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "lowercase",
			input:    "server",
			expected: "server",
		},
		{
			name:     "uppercase",
			input:    "SERVER",
			expected: "server",
		},
		{
			name:     "mixed case",
			input:    "MyServer",
			expected: "myserver",
		},
		{
			name:     "with hyphens",
			input:    "my-server",
			expected: "my-server",
		},
		{
			name:     "with underscores",
			input:    "my_server",
			expected: "my-server",
		},
		{
			name:     "with dots",
			input:    "my.server",
			expected: "my.server",
		},
		{
			name:     "with special chars",
			input:    "my@server#1",
			expected: "my-server-1",
		},
		{
			name:     "leading hyphen",
			input:    "-server",
			expected: "server",
		},
		{
			name:     "trailing hyphen",
			input:    "server-",
			expected: "server",
		},
		{
			name:     "leading underscore",
			input:    "_server",
			expected: "server",
		},
		{
			name:     "trailing underscore",
			input:    "server_",
			expected: "server",
		},
		{
			name:     "leading dot",
			input:    ".server",
			expected: "server",
		},
		{
			name:     "trailing dot",
			input:    "server.",
			expected: "server",
		},
		{
			name:     "multiple leading/trailing",
			input:    "_.server._",
			expected: "server",
		},
		{
			name:     "complex sanitization",
			input:    "_My@Server#123_",
			expected: "my-server-123",
		},
		{
			name:     "only special chars",
			input:    "---",
			expected: "",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := sanitizeNamespace(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeNamespace(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSanitizeName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "lowercase",
			input:    "server",
			expected: "server",
		},
		{
			name:     "uppercase",
			input:    "SERVER",
			expected: "server",
		},
		{
			name:     "with hyphens",
			input:    "my-server",
			expected: "my-server",
		},
		{
			name:     "with underscores - kept in name",
			input:    "my_server",
			expected: "my_server",
		},
		{
			name:     "with dots",
			input:    "my.server",
			expected: "my.server",
		},
		{
			name:     "with special chars",
			input:    "my@server#1",
			expected: "my-server-1",
		},
		{
			name:     "leading hyphen",
			input:    "-server",
			expected: "server",
		},
		{
			name:     "trailing hyphen",
			input:    "server-",
			expected: "server",
		},
		{
			name:     "leading underscore",
			input:    "_server",
			expected: "server",
		},
		{
			name:     "trailing underscore",
			input:    "server_",
			expected: "server",
		},
		{
			name:     "underscores in middle - preserved",
			input:    "my_test_server",
			expected: "my_test_server",
		},
		{
			name:     "mixed case with underscores",
			input:    "My_Server",
			expected: "my_server",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := sanitizeName(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
