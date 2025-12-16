package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestNewProtectedResourceHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		resourceURL     string
		authServers     []string
		scopes          []string
		wantResource    string
		wantAuthServers []string
		wantScopes      []string
	}{
		{
			name:            "full configuration",
			resourceURL:     "https://api.example.com",
			authServers:     []string{"https://auth.example.com"},
			scopes:          []string{"read", "write"},
			wantResource:    "https://api.example.com",
			wantAuthServers: []string{"https://auth.example.com"},
			wantScopes:      []string{"read", "write"},
		},
		{
			name:            "default scopes applied when nil",
			resourceURL:     "https://api.example.com",
			authServers:     []string{"https://auth.example.com"},
			scopes:          nil,
			wantResource:    "https://api.example.com",
			wantAuthServers: []string{"https://auth.example.com"},
			wantScopes:      config.DefaultScopes,
		},
		{
			name:            "default scopes applied when empty",
			resourceURL:     "https://api.example.com",
			authServers:     []string{"https://auth.example.com"},
			scopes:          []string{},
			wantResource:    "https://api.example.com",
			wantAuthServers: []string{"https://auth.example.com"},
			wantScopes:      config.DefaultScopes,
		},
		{
			name:            "multiple auth servers",
			resourceURL:     "https://api.example.com",
			authServers:     []string{"https://auth1.example.com", "https://auth2.example.com"},
			scopes:          []string{"admin"},
			wantResource:    "https://api.example.com",
			wantAuthServers: []string{"https://auth1.example.com", "https://auth2.example.com"},
			wantScopes:      []string{"admin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler, err := newProtectedResourceHandler(tt.resourceURL, tt.authServers, tt.scopes)
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
			assert.Equal(t, tt.wantScopes, metadata.ScopesSupported)
			assert.Contains(t, metadata.BearerMethodsSupported, "header")
		})
	}
}

func TestNewProtectedResourceHandler_ValidationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		resourceURL string
		authServers []string
		scopes      []string
		wantErr     string
	}{
		{
			name:        "nil auth servers",
			resourceURL: "https://api.example.com",
			authServers: nil,
			scopes:      []string{"read"},
			wantErr:     "at least one authorization server is required",
		},
		{
			name:        "empty auth servers",
			resourceURL: "https://api.example.com",
			authServers: []string{},
			scopes:      []string{"read"},
			wantErr:     "at least one authorization server is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler, err := newProtectedResourceHandler(tt.resourceURL, tt.authServers, tt.scopes)
			require.Error(t, err)
			assert.Nil(t, handler)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
