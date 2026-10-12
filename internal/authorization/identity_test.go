package authorization

import (
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestPrincipalIdentity(t *testing.T) {
	t.Parallel()
	mappings := []config.IdentityMapping{{Issuer: "https://issuer", Groups: &config.AttributeMapping{Claim: "roles", Format: "string-array"}, Scopes: &config.AttributeMapping{Claim: "scope", Format: "space-delimited"}}}
	identities := []auth.VerifiedIdentity{
		{Issuer: "https://issuer", Subject: "same", Claims: jwt.MapClaims{"iss": "https://issuer", "sub": "same"}},
		{Issuer: "https://other", Subject: "same", Claims: jwt.MapClaims{"iss": "https://other", "sub": "same"}},
		{Issuer: "https://issuer", Subject: "anonymous", Claims: jwt.MapClaims{"iss": "https://issuer", "sub": "anonymous"}},
		{Anonymous: true},
	}
	ids := map[string]bool{}
	for _, identity := range identities {
		p, err := principal(identity, mappings)
		require.NoError(t, err)
		require.False(t, ids[string(p.UID.ID)])
		ids[string(p.UID.ID)] = true
	}
	for _, tc := range []struct {
		name  string
		extra jwt.MapClaims
		valid bool
	}{
		{"absent", nil, true}, {"groups", jwt.MapClaims{"roles": []any{"readers"}}, true},
		{"scopes", jwt.MapClaims{"scope": "read write"}, true},
		{"empty set", jwt.MapClaims{"roles": []string{}}, true},
		{"null", jwt.MapClaims{"roles": nil}, false}, {"wrong type", jwt.MapClaims{"roles": "readers"}, false},
		{"mixed types", jwt.MapClaims{"roles": []any{"readers", 1}}, false},
		{"too many", jwt.MapClaims{"roles": make([]string, 257)}, false},
		{"oversized", jwt.MapClaims{"roles": []string{strings.Repeat("x", 4097)}}, false},
		{"reserved", jwt.MapClaims{"authenticated": true}, false},
		{"issuer mismatch", jwt.MapClaims{"iss": "https://other"}, false},
		{"no subject", jwt.MapClaims{"sub": ""}, false},
		{"bad scope", jwt.MapClaims{"scope": []string{"read"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			claims := jwt.MapClaims{"iss": "https://issuer", "sub": "same"}
			for key, value := range tc.extra {
				claims[key] = value
			}
			_, err := principal(auth.VerifiedIdentity{Issuer: "https://issuer", Subject: "same", Claims: claims}, mappings)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
