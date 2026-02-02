package service_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/service"
)

func TestEncodeCursor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		inName   string
		version  string
		expected string
	}{
		{
			name:     "simple name and version",
			inName:   "server1",
			version:  "1.0.0",
			expected: "c2VydmVyMSwxLjAuMA==", // base64("server1,1.0.0")
		},
		{
			name:     "name with special characters",
			inName:   "@org/server",
			version:  "2.0.0-beta",
			expected: "QG9yZy9zZXJ2ZXIsMi4wLjAtYmV0YQ==", // base64("@org/server,2.0.0-beta")
		},
		{
			name:     "empty name",
			inName:   "",
			version:  "1.0.0",
			expected: "LDEuMC4w", // base64(",1.0.0")
		},
		{
			name:     "empty version",
			inName:   "server",
			version:  "",
			expected: "c2VydmVyLA==", // base64("server,")
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := service.EncodeCursor(tt.inName, tt.version)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDecodeCursor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		cursor          string
		expectedName    string
		expectedVersion string
		expectError     bool
		errorContains   string
	}{
		{
			name:            "valid cursor",
			cursor:          "c2VydmVyMSwxLjAuMA==", // base64("server1,1.0.0")
			expectedName:    "server1",
			expectedVersion: "1.0.0",
			expectError:     false,
		},
		{
			name:            "empty cursor returns empty strings",
			cursor:          "",
			expectedName:    "",
			expectedVersion: "",
			expectError:     false,
		},
		{
			name:            "cursor with special characters in name",
			cursor:          "QG9yZy9zZXJ2ZXIsMi4wLjAtYmV0YQ==", // base64("@org/server,2.0.0-beta")
			expectedName:    "@org/server",
			expectedVersion: "2.0.0-beta",
			expectError:     false,
		},
		{
			name:          "invalid base64 returns error",
			cursor:        "not-valid-base64!!!",
			expectError:   true,
			errorContains: "failed to decode cursor",
		},
		{
			name:          "valid base64 but no comma separator returns error",
			cursor:        "YWJj", // base64("abc")
			expectError:   true,
			errorContains: "invalid cursor format: expected 2 fields separated by comma",
		},
		{
			name:            "cursor with colons in timestamp-style version works",
			cursor:          "c2VydmVyLDIwMjQtMDE6MzA6MDA=", // base64("server,2024-01:30:00")
			expectedName:    "server",
			expectedVersion: "2024-01:30:00",
			expectError:     false,
		},
		{
			name:          "cursor with multiple commas returns error",
			cursor:        "c2VydmVyLDEuMC4wLGV4dHJh", // base64("server,1.0.0,extra")
			expectError:   true,
			errorContains: "invalid cursor format: expected 2 fields separated by comma",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			name, version, err := service.DecodeCursor(tt.cursor)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedName, name)
				assert.Equal(t, tt.expectedVersion, version)
			}
		})
	}
}

func TestCursorRoundTrip(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		inName  string
		version string
	}{
		{"simple", "server1", "1.0.0"},
		{"scoped package", "@stacklok/my-server", "2.0.0-alpha.1"},
		{"empty version", "server", ""},
		{"unicode name", "server-\u00e9", "1.0.0"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Encode
			cursor := service.EncodeCursor(tc.inName, tc.version)

			// Decode
			decodedName, decodedVersion, err := service.DecodeCursor(cursor)
			require.NoError(t, err)

			// Verify round trip
			assert.Equal(t, tc.inName, decodedName)
			assert.Equal(t, tc.version, decodedVersion)
		})
	}
}
