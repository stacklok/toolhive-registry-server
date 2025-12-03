package validators

import (
	"strings"
	"testing"
)

func TestValidateServerName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		serverName  string
		expectValid bool
		expectError string
	}{
		// Valid cases
		{
			name:        "simple valid name",
			serverName:  "com.example/server",
			expectValid: true,
		},
		{
			name:        "valid with hyphens",
			serverName:  "org.stacklok.toolhive/my-server",
			expectValid: true,
		},
		{
			name:        "valid with underscores in name",
			serverName:  "io.github.user/test_server",
			expectValid: true,
		},
		{
			name:        "valid with dots in name",
			serverName:  "com.example/server.prod",
			expectValid: true,
		},
		{
			name:        "valid k8s format",
			serverName:  "com.toolhive.k8s.default/weather-service",
			expectValid: true,
		},
		{
			name:        "valid with multiple dots in namespace",
			serverName:  "com.example.api.v1/server",
			expectValid: true,
		},
		{
			name:        "valid with mixed characters",
			serverName:  "org.example/server-1.test_v2",
			expectValid: true,
		},
		{
			name:        "minimum valid length",
			serverName:  "a/b",
			expectValid: true,
		},
		{
			name:        "valid with numbers",
			serverName:  "com.example123/server456",
			expectValid: true,
		},
		{
			name:        "valid starting with number",
			serverName:  "1example/2server",
			expectValid: true,
		},

		// Invalid cases - missing slash
		{
			name:        "no slash",
			serverName:  "my-server",
			expectValid: false,
			expectError: "must be in format 'dns-namespace/name'",
		},

		// Invalid cases - multiple slashes
		{
			name:        "multiple slashes",
			serverName:  "com.example//server",
			expectValid: false,
			expectError: "must contain exactly one '/' separator",
		},
		{
			name:        "three slashes",
			serverName:  "com/example/server",
			expectValid: false,
			expectError: "must contain exactly one '/' separator",
		},

		// Invalid cases - empty parts
		{
			name:        "empty string",
			serverName:  "",
			expectValid: false,
			expectError: "cannot be empty",
		},
		{
			name:        "only slash",
			serverName:  "/",
			expectValid: false,
			expectError: "namespace part cannot be empty",
		},
		{
			name:        "empty namespace",
			serverName:  "/server",
			expectValid: false,
			expectError: "namespace part cannot be empty",
		},
		{
			name:        "empty name",
			serverName:  "com.example/",
			expectValid: false,
			expectError: "name part cannot be empty",
		},

		// Invalid cases - length constraints
		{
			name:        "too short",
			serverName:  "a/",
			expectValid: false,
			expectError: "name part cannot be empty",
		},
		{
			name:        "exceeds max length",
			serverName:  strings.Repeat("a", 150) + "/" + strings.Repeat("b", 60),
			expectValid: false,
			expectError: "exceeds maximum length of 200 characters",
		},

		// Invalid cases - namespace validation
		{
			name:        "namespace starts with dot",
			serverName:  ".example/server",
			expectValid: false,
			expectError: "namespace '.example' is invalid",
		},
		{
			name:        "namespace ends with dot",
			serverName:  "example./server",
			expectValid: false,
			expectError: "namespace 'example.' is invalid",
		},
		{
			name:        "namespace starts with hyphen",
			serverName:  "-example/server",
			expectValid: false,
			expectError: "namespace '-example' is invalid",
		},
		{
			name:        "namespace ends with hyphen",
			serverName:  "example-/server",
			expectValid: false,
			expectError: "namespace 'example-' is invalid",
		},
		{
			name:        "namespace with underscore",
			serverName:  "my_namespace/server",
			expectValid: false,
			expectError: "namespace 'my_namespace' is invalid",
		},
		{
			name:        "namespace with special characters",
			serverName:  "com.example@prod/server",
			expectValid: false,
			expectError: "namespace 'com.example@prod' is invalid",
		},

		// Invalid cases - name validation
		{
			name:        "name starts with dot",
			serverName:  "com.example/.server",
			expectValid: false,
			expectError: "name '.server' is invalid",
		},
		{
			name:        "name ends with dot",
			serverName:  "com.example/server.",
			expectValid: false,
			expectError: "name 'server.' is invalid",
		},
		{
			name:        "name starts with hyphen",
			serverName:  "com.example/-server",
			expectValid: false,
			expectError: "name '-server' is invalid",
		},
		{
			name:        "name ends with hyphen",
			serverName:  "com.example/server-",
			expectValid: false,
			expectError: "name 'server-' is invalid",
		},
		{
			name:        "name starts with underscore",
			serverName:  "com.example/_server",
			expectValid: false,
			expectError: "name '_server' is invalid",
		},
		{
			name:        "name ends with underscore",
			serverName:  "com.example/server_",
			expectValid: false,
			expectError: "name 'server_' is invalid",
		},
		{
			name:        "name with special characters",
			serverName:  "com.example/server@prod",
			expectValid: false,
			expectError: "name 'server@prod' is invalid",
		},
		{
			name:        "name with spaces",
			serverName:  "com.example/my server",
			expectValid: false,
			expectError: "name 'my server' is invalid",
		},

		// Edge cases - whitespace handling
		{
			name:        "leading whitespace",
			serverName:  "  com.example/server",
			expectValid: true, // Should be trimmed
		},
		{
			name:        "trailing whitespace",
			serverName:  "com.example/server  ",
			expectValid: true, // Should be trimmed
		},
		{
			name:        "whitespace only",
			serverName:  "   ",
			expectValid: false,
			expectError: "cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := ValidateServerName(tt.serverName)

			if tt.expectValid {
				if err != nil {
					t.Errorf("Expected valid, got error: %v", err)
				}
				if result == "" {
					t.Errorf("Expected non-empty result for valid name")
				}
				// Verify trimming
				if result != strings.TrimSpace(tt.serverName) {
					t.Errorf("Expected result to be trimmed: got %q, want %q", result, strings.TrimSpace(tt.serverName))
				}
			} else {
				if err == nil {
					t.Errorf("Expected error, got nil")
				}
				if tt.expectError != "" && !strings.Contains(err.Error(), tt.expectError) {
					t.Errorf("Expected error containing %q, got %q", tt.expectError, err.Error())
				}
			}
		})
	}
}

func TestIsValidServerName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		serverName  string
		expectValid bool
	}{
		{
			name:        "valid name",
			serverName:  "com.example/server",
			expectValid: true,
		},
		{
			name:        "invalid name - no slash",
			serverName:  "my-server",
			expectValid: false,
		},
		{
			name:        "invalid name - empty",
			serverName:  "",
			expectValid: false,
		},
		{
			name:        "valid k8s format",
			serverName:  "com.toolhive.k8s.default/weather-service",
			expectValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := IsValidServerName(tt.serverName)
			if result != tt.expectValid {
				t.Errorf("IsValidServerName(%q) = %v, want %v", tt.serverName, result, tt.expectValid)
			}
		})
	}
}
