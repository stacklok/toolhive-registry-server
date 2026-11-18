package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestAnonymousChallenge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  *config.AuthConfig
		want string
	}{
		{
			name: "nil config uses the default realm",
			want: `Bearer realm="mcp-registry"`,
		},
		{
			name: "anonymous mode uses the default realm",
			cfg:  &config.AuthConfig{Mode: config.AuthModeAnonymous},
			want: `Bearer realm="mcp-registry"`,
		},
		{
			name: "oauth without realm or resource uses the default realm",
			cfg:  &config.AuthConfig{Mode: config.AuthModeOAuth, OAuth: &config.OAuthConfig{}},
			want: `Bearer realm="mcp-registry"`,
		},
		{
			name: "custom realm",
			cfg:  &config.AuthConfig{Mode: config.AuthModeOAuth, OAuth: &config.OAuthConfig{Realm: "corp"}},
			want: `Bearer realm="corp"`,
		},
		{
			name: "resource URL adds the protected resource metadata",
			cfg: &config.AuthConfig{Mode: config.AuthModeOAuth, OAuth: &config.OAuthConfig{
				ResourceURL: "https://registry.example.com"}},
			want: `Bearer realm="mcp-registry", ` +
				`resource_metadata="https://registry.example.com/.well-known/oauth-protected-resource"`,
		},
		{
			name: "custom realm and resource URL",
			cfg: &config.AuthConfig{Mode: config.AuthModeOAuth, OAuth: &config.OAuthConfig{
				Realm: "corp", ResourceURL: "https://registry.example.com"}},
			want: `Bearer realm="corp", ` +
				`resource_metadata="https://registry.example.com/.well-known/oauth-protected-resource"`,
		},
		{
			name: "realm and resource URL are sanitized",
			cfg: &config.AuthConfig{Mode: config.AuthModeOAuth, OAuth: &config.OAuthConfig{
				Realm: "a\"b\r\nX-Injected: 1", ResourceURL: "https://r.example\"\n.com"}},
			want: `Bearer realm="a\"bX-Injected: 1", ` +
				`resource_metadata="https://r.example\".com/.well-known/oauth-protected-resource"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			challenge := AnonymousChallenge(tt.cfg)
			got := challenge(httptest.NewRequest(http.MethodGet, "/v1/sources", nil))
			assert.Equal(t, tt.want, got)
			assert.NotContains(t, got, "\r")
			assert.NotContains(t, got, "\n")
		})
	}
}
