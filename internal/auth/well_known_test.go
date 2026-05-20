package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProtectedResourceHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		resourceURL     string
		authServers     []string
		wantResource    string
		wantAuthServers []string
	}{
		{
			name:            "full configuration",
			resourceURL:     "https://api.example.com",
			authServers:     []string{"https://auth.example.com"},
			wantResource:    "https://api.example.com",
			wantAuthServers: []string{"https://auth.example.com"},
		},
		{
			name:            "multiple auth servers",
			resourceURL:     "https://api.example.com",
			authServers:     []string{"https://auth1.example.com", "https://auth2.example.com"},
			wantResource:    "https://api.example.com",
			wantAuthServers: []string{"https://auth1.example.com", "https://auth2.example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler, err := newProtectedResourceHandler(tt.resourceURL, tt.authServers)
			require.NoError(t, err)
			require.NotNil(t, handler)

			req := httptest.NewRequest("GET", "/.well-known/oauth-protected-resource", nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusOK, rr.Code)
			assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

			var metadata protectedResourceMetadata
			err = json.Unmarshal(rr.Body.Bytes(), &metadata)
			require.NoError(t, err)

			assert.Equal(t, tt.wantResource, metadata.Resource)
			assert.Equal(t, tt.wantAuthServers, metadata.AuthorizationServers)
			assert.Contains(t, metadata.BearerMethodsSupported, "header")

			// Verify scopes_supported is not emitted.
			var raw map[string]any
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &raw))
			_, hasScopes := raw["scopes_supported"]
			assert.False(t, hasScopes, "scopes_supported should not be present in metadata")
		})
	}
}

func TestNewProtectedResourceHandler_ValidationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		resourceURL string
		authServers []string
		wantErr     string
	}{
		{
			name:        "nil auth servers",
			resourceURL: "https://api.example.com",
			authServers: nil,
			wantErr:     "at least one authorization server is required",
		},
		{
			name:        "empty auth servers",
			resourceURL: "https://api.example.com",
			authServers: []string{},
			wantErr:     "at least one authorization server is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler, err := newProtectedResourceHandler(tt.resourceURL, tt.authServers)
			require.Error(t, err)
			assert.Nil(t, handler)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
