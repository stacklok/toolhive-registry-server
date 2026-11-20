package cedar

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cedar-policy/cedar-go/types"
	"github.com/stretchr/testify/require"
)

const testIssuer = "https://id.example.org"

var testMappings = []Mapping{{
	Issuer: testIssuer,
	Groups: &AttributeMapping{Claim: "roles", Format: FormatStringArray},
	Scopes: &AttributeMapping{Claim: "scope", Format: FormatSpaceDelimited},
}}

func verified(subject string, extra map[string]any) Identity {
	claims := map[string]any{"iss": testIssuer, "sub": subject}
	for k, v := range extra {
		claims[k] = v
	}
	return Identity{Issuer: testIssuer, Subject: subject, Claims: claims}
}

func setStrings(t *testing.T, v types.Value) []string {
	t.Helper()
	set, ok := v.(types.Set)
	require.True(t, ok, "attribute is not a set")
	var out []string
	for item := range set.All() {
		out = append(out, string(item.(types.String)))
	}
	return out
}

func TestPrincipalAttributes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		extra  map[string]any
		groups []string // nil means attribute absent
		scopes []string
	}{
		{name: "claims absent leave attributes absent"},
		{name: "string-array from []any", extra: map[string]any{"roles": []any{"readers", "admins"}},
			groups: []string{"readers", "admins"}},
		{name: "string-array from []string", extra: map[string]any{"roles": []string{"readers"}},
			groups: []string{"readers"}},
		{name: "empty array is an empty set", extra: map[string]any{"roles": []string{}}, groups: []string{}},
		{name: "space-delimited", extra: map[string]any{"scope": "read write"}, scopes: []string{"read", "write"}},
		{name: "empty space-delimited is an empty set", extra: map[string]any{"scope": ""}, scopes: []string{}},
		{name: "both", extra: map[string]any{"roles": []any{"a"}, "scope": "b"}, groups: []string{"a"}, scopes: []string{"b"}},
		{name: "256 values accepted", extra: map[string]any{"roles": distinctValues(256)}, groups: distinctValues(256)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p, err := principal(verified("user", tc.extra), testMappings)
			require.NoError(t, err)
			require.Equal(t, types.String(PrincipalID(testIssuer, "user")), p.UID.ID)
			require.Equal(t, types.Boolean(true), p.Attributes.Map()["authenticated"])
			require.Equal(t, types.String(testIssuer), p.Attributes.Map()["issuer"])
			require.Equal(t, types.String("user"), p.Attributes.Map()["subject"])
			for name, want := range map[types.String][]string{"groups": tc.groups, "scopes": tc.scopes} {
				got, ok := p.Attributes.Get(name)
				if want == nil {
					require.False(t, ok, "%s must be absent", name)
					continue
				}
				require.True(t, ok, "%s must be present", name)
				require.ElementsMatch(t, want, setStrings(t, got))
			}
		})
	}
}

func TestPrincipalRejectsMalformedIdentity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		identity Identity
	}{
		{name: "null mapped claim", identity: verified("user", map[string]any{"roles": nil})},
		{name: "string-array given a string", identity: verified("user", map[string]any{"roles": "readers"})},
		{name: "string-array with a number", identity: verified("user", map[string]any{"roles": []any{"readers", 1}})},
		{name: "string-array over 256 values", identity: verified("user", map[string]any{"roles": repeatValues(257, "g")})},
		{name: "string-array []any over 256 values", identity: verified("user", map[string]any{"roles": anyValues(257)})},
		{name: "value over 4096 bytes", identity: verified("user", map[string]any{"roles": []string{strings.Repeat("x", 4097)}})},
		{name: "total over 16 KiB", identity: verified("user", map[string]any{"roles": repeatValues(5, strings.Repeat("x", 4000))})},
		{name: "empty value", identity: verified("user", map[string]any{"roles": []string{""}})},
		{name: "value with newline", identity: verified("user", map[string]any{"roles": []string{"a\nb"}})},
		{name: "space-delimited given an array", identity: verified("user", map[string]any{"scope": []string{"read"}})},
		{name: "space-delimited with tab", identity: verified("user", map[string]any{"scope": "read\twrite"})},
		{name: "space-delimited double space", identity: verified("user", map[string]any{"scope": "read  write"})},
		{name: "space-delimited over 16 KiB", identity: verified("user", map[string]any{"scope": strings.Repeat("x", maxClaimBytes+1)})},
		{name: "space-delimited invalid utf-8", identity: verified("user", map[string]any{"scope": "\xff"})},
		{name: "reserved claim authenticated", identity: verified("user", map[string]any{"authenticated": true})},
		{name: "reserved claim issuer", identity: verified("user", map[string]any{"issuer": "x"})},
		{name: "reserved claim subject", identity: verified("user", map[string]any{"subject": "x"})},
		{name: "iss claim mismatch", identity: verified("user", map[string]any{"iss": "https://other.example.org"})},
		{name: "sub claim mismatch", identity: verified("user", map[string]any{"sub": "other"})},
		{name: "iss claim not a string", identity: verified("user", map[string]any{"iss": 1})},
		{name: "claims nil", identity: Identity{Issuer: testIssuer, Subject: "user"}},
		{name: "empty subject", identity: Identity{Issuer: testIssuer, Subject: "",
			Claims: map[string]any{"iss": testIssuer, "sub": ""}}},
		{name: "subject with NUL", identity: verified("a\x00b", nil)},
		{name: "subject over 4096 bytes", identity: verified(strings.Repeat("s", 4097), nil)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := principal(tc.identity, testMappings)
			require.ErrorIs(t, err, ErrInvalidIdentity)
		})
	}
}

func TestPrincipalMappingAppliesOnlyToItsIssuer(t *testing.T) {
	t.Parallel()
	other := Identity{Issuer: "https://other.example.org", Subject: "user", Claims: map[string]any{
		"iss": "https://other.example.org", "sub": "user", "roles": "not-an-array",
	}}
	p, err := principal(other, testMappings)
	require.NoError(t, err, "unmapped issuer must ignore the mapped claim, even if malformed")
	_, ok := p.Attributes.Get("groups")
	require.False(t, ok)
}

func TestPrincipalAnonymous(t *testing.T) {
	t.Parallel()
	// Claims on an anonymous identity are ignored: no mappings, no checks.
	p, err := principal(Identity{Anonymous: true, Claims: map[string]any{"roles": 1}}, testMappings)
	require.NoError(t, err)
	require.Equal(t, types.String(AnonymousPrincipal), p.UID.ID)
	require.Equal(t, types.Boolean(false), p.Attributes.Map()["authenticated"])
	require.Equal(t, types.String(""), p.Attributes.Map()["issuer"])
	_, ok := p.Attributes.Get("groups")
	require.False(t, ok)
}

func TestPrincipalIDsAreDistinct(t *testing.T) {
	t.Parallel()
	ids := map[types.String]bool{}
	for _, identity := range []Identity{
		verified("same", nil),
		{Issuer: "https://other.example.org", Subject: "same", Claims: map[string]any{"iss": "https://other.example.org", "sub": "same"}},
		verified("anonymous", nil),
		{Anonymous: true},
	} {
		p, err := principal(identity, nil)
		require.NoError(t, err)
		require.False(t, ids[p.UID.ID], "duplicate principal ID %s", p.UID.ID)
		ids[p.UID.ID] = true
	}
}

func TestPrincipalID(t *testing.T) {
	t.Parallel()
	tuple, err := json.Marshal([]string{testIssuer, "user"})
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("authenticated:%x", sha256.Sum256(tuple)), PrincipalID(testIssuer, "user"))
	require.Equal(t, PrincipalID(testIssuer, "user"), PrincipalID(testIssuer, "user"))
	// The JSON tuple prevents delimiter collisions.
	require.NotEqual(t, PrincipalID("a:b", "c"), PrincipalID("a", "b:c"))
	require.NotEqual(t, PrincipalID(`a","b`, "c"), PrincipalID("a", `b","c`))
	for _, bad := range [][2]string{
		{"", "user"}, {testIssuer, ""}, {testIssuer, "a\rb"}, {"\xff", "user"}, {testIssuer, strings.Repeat("x", 4097)},
	} {
		require.Empty(t, PrincipalID(bad[0], bad[1]), "%q", bad)
	}
}

func TestValidateMappings(t *testing.T) {
	t.Parallel()
	issuers := []string{testIssuer, "https://other.example.org"}
	groups := func(claim string, format Format) *AttributeMapping {
		return &AttributeMapping{Claim: claim, Format: format}
	}
	tests := []struct {
		name     string
		mappings []Mapping
		valid    bool
	}{
		{name: "none", valid: true},
		{name: "one issuer, both attributes", mappings: testMappings, valid: true},
		{name: "two issuers", valid: true, mappings: []Mapping{
			{Issuer: testIssuer, Groups: groups("groups", FormatStringArray)},
			{Issuer: "https://other.example.org", Scopes: groups("scp", FormatSpaceDelimited)},
		}},
		{name: "no attributes", mappings: []Mapping{{Issuer: testIssuer}}, valid: true},
		{name: "128-char claim", mappings: []Mapping{{Issuer: testIssuer, Groups: groups(strings.Repeat("c", 128), FormatStringArray)}}, valid: true},
		{name: "unknown issuer", mappings: []Mapping{{Issuer: "https://unknown.example.org"}}},
		{name: "empty issuer", mappings: []Mapping{{Issuer: ""}}},
		{name: "duplicate issuer", mappings: []Mapping{{Issuer: testIssuer}, {Issuer: testIssuer}}},
		{name: "empty claim", mappings: []Mapping{{Issuer: testIssuer, Groups: groups("", FormatStringArray)}}},
		{name: "129-char claim", mappings: []Mapping{{Issuer: testIssuer, Groups: groups(strings.Repeat("c", 129), FormatStringArray)}}},
		{name: "leading whitespace", mappings: []Mapping{{Issuer: testIssuer, Groups: groups(" groups", FormatStringArray)}}},
		{name: "trailing whitespace", mappings: []Mapping{{Issuer: testIssuer, Scopes: groups("scope\t", FormatSpaceDelimited)}}},
		{name: "reserved iss", mappings: []Mapping{{Issuer: testIssuer, Groups: groups("iss", FormatStringArray)}}},
		{name: "reserved sub", mappings: []Mapping{{Issuer: testIssuer, Groups: groups("sub", FormatStringArray)}}},
		{name: "reserved authenticated", mappings: []Mapping{{Issuer: testIssuer, Scopes: groups("authenticated", FormatSpaceDelimited)}}},
		{name: "reserved issuer", mappings: []Mapping{{Issuer: testIssuer, Groups: groups("issuer", FormatStringArray)}}},
		{name: "reserved subject", mappings: []Mapping{{Issuer: testIssuer, Groups: groups("subject", FormatStringArray)}}},
		{name: "unknown format", mappings: []Mapping{{Issuer: testIssuer, Groups: groups("groups", "csv")}}},
		{name: "empty format", mappings: []Mapping{{Issuer: testIssuer, Groups: groups("groups", "")}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateMappings(issuers, tc.mappings)
			if tc.valid {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
		})
	}
	require.Error(t, ValidateMappings(nil, testMappings), "no configured issuers")
}

func repeatValues(n int, value string) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = value
	}
	return out
}

func distinctValues(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("g%d", i)
	}
	return out
}

func anyValues(n int) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = "g"
	}
	return out
}
