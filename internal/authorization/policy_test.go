package authorization

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cedar-policy/cedar-go"
	"github.com/cedar-policy/cedar-go/types"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

const permitCatalog = `permit(principal is Registry::Principal, action == Registry::Action::"catalog.read", resource == Registry::View::"engineering");`

func testResource(action Action) types.EntityUID {
	if action == CatalogRead {
		return types.EntityUID{Type: "Registry::View", ID: "engineering"}
	}
	return instance
}

func TestPolicyLoader(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, policy string
		valid        bool
	}{
		{"empty", "", false}, {"comments", "// nothing\n/* nothing */", false},
		{"syntax", "permit(", false},
		{"unknown action", `permit(principal, action == Registry::Action::"root", resource);`, false},
		{"old instance scoped catalog read", `permit(principal, action == Registry::Action::"catalog.read", resource == Registry::Instance::"catalog");`, false},
		{"type error", permitCatalog[:len(permitCatalog)-1] + ` when { principal.authenticated == "yes" };`, false},
		{"unknown attribute", permitCatalog[:len(permitCatalog)-1] + ` when { principal.password == "yes" };`, false},
		{"unsafe optional", permitCatalog[:len(permitCatalog)-1] + ` when { principal.groups.contains("readers") };`, false},
		{"valid", permitCatalog, true}, {"oversize", strings.Repeat(" ", maxPolicyBytes+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "policy.cedar")
			require.NoError(t, os.WriteFile(path, []byte(tc.policy), 0600))
			engine, err := Load(path)
			if !tc.valid {
				require.Error(t, err)
				require.Nil(t, engine)
				return
			}
			require.NoError(t, err)
			anonymous, err := principal(auth.VerifiedIdentity{Anonymous: true}, nil)
			require.NoError(t, err)
			require.NoError(t, engine.evaluate(anonymous, CatalogRead, testResource(CatalogRead)))
			revision := engine.revision
			require.Equal(t, fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(tc.policy))), engine.Revision())
			require.NoError(t, os.WriteFile(path, []byte(`forbid(principal, action, resource);`), 0600))
			require.NoError(t, engine.evaluate(anonymous, CatalogRead, testResource(CatalogRead)))
			require.Equal(t, revision, engine.revision)
			restarted, err := Load(path)
			require.NoError(t, err)
			require.ErrorIs(t, restarted.evaluate(anonymous, CatalogRead, testResource(CatalogRead)), ErrDenied)
		})
	}
	_, err := Load("relative.cedar")
	require.Error(t, err)
	_, err = Load(filepath.Join(t.TempDir(), "missing"))
	require.Error(t, err)
	_, err = Load(t.TempDir())
	require.Error(t, err)
}

func TestPolicyDecisions(t *testing.T) {
	t.Parallel()
	p, err := principal(auth.VerifiedIdentity{Anonymous: true}, nil)
	require.NoError(t, err)
	for _, tc := range []struct {
		name, policy string
		action       Action
		want         error
	}{
		{"permit", permitCatalog, CatalogRead, nil},
		{"no match", permitCatalog, SourcesWrite, ErrDenied},
		{"forbid wins", permitCatalog + `forbid(principal, action, resource);`, CatalogRead, ErrDenied},
		{"unknown closed action", permitCatalog, "root", ErrEvidence},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			engine, err := parse([]byte(tc.policy))
			require.NoError(t, err)
			require.ErrorIs(t, engine.evaluate(p, tc.action, testResource(tc.action)), tc.want)
		})
	}
	// Explicitly simulate an evaluator diagnostic; production Load would reject
	// this policy. Cedar can return Allow while another policy fails evaluation.
	ps, err := cedar.NewPolicySetFromBytes("test", []byte(permitCatalog+`permit(principal, action, resource) when { principal.missing == true };`))
	require.NoError(t, err)
	engine := &Engine{policies: ps}
	decision, diagnostic := cedar.Authorize(ps, types.EntityMap{p.UID: p}, cedar.Request{Principal: p.UID, Action: types.EntityUID{Type: "Registry::Action", ID: "catalog.read"}, Resource: testResource(CatalogRead)})
	require.Equal(t, cedar.Allow, decision)
	require.NotEmpty(t, diagnostic.Errors)
	require.ErrorIs(t, engine.evaluate(p, CatalogRead, testResource(CatalogRead)), ErrEvaluation)
	require.ErrorIs(t, (*Engine)(nil).evaluate(p, CatalogRead, testResource(CatalogRead)), ErrEvaluation)
}

func TestDocumentedPolicies(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../docs/registry-cedar-authorization.md")
	require.NoError(t, err)
	var policies []byte
	for _, block := range bytes.Split(data, []byte("```cedar\n"))[1:] {
		end := bytes.Index(block, []byte("```"))
		require.GreaterOrEqual(t, end, 0)
		policies = append(policies, block[:end]...)
	}
	engine, err := parse(policies)
	require.NoError(t, err)
	identity := auth.VerifiedIdentity{Issuer: "https://id.example.org", Subject: "reader", Claims: map[string]any{"iss": "https://id.example.org", "sub": "reader", "groups": []any{"engineering"}}}
	p, err := principal(identity, []config.IdentityMapping{{Issuer: identity.Issuer, Groups: &config.AttributeMapping{Claim: "groups", Format: "string-array"}}})
	require.NoError(t, err)
	require.NoError(t, engine.evaluate(p, CatalogRead, testResource(CatalogRead)))
	require.ErrorIs(t, engine.evaluate(p, EntriesPublish, instance), ErrDenied)
}

func TestMissingFacadeEvidence(t *testing.T) {
	t.Parallel()
	engine, err := parse([]byte(permitCatalog))
	require.NoError(t, err)
	gate, err := NewRegistrar(engine, nil, nil)
	require.NoError(t, err)
	require.ErrorIs(t, gate.admit(context.Background(), CatalogRead), ErrEvidence)
}
