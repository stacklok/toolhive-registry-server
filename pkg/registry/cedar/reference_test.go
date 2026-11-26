package cedar

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	cedargo "github.com/cedar-policy/cedar-go"
	"github.com/cedar-policy/cedar-go/types"
	"github.com/stretchr/testify/require"
)

// TestOfficialReferenceParity compares cedar-go and Parse with decisions the
// official Rust cedar-policy crate produced for the same schema and cases. See
// testdata/reference/README.md for how results.json is regenerated.
func TestOfficialReferenceParity(t *testing.T) {
	t.Parallel()
	var reference struct {
		Reference    string `json:"reference"`
		SchemaSHA256 string `json:"schema_sha256"`
		CasesSHA256  string `json:"cases_sha256"`
		Results      []struct {
			Name     string `json:"name"`
			Decision string `json:"decision"`
			Valid    bool   `json:"valid"`
			Errors   int    `json:"errors"`
		} `json:"results"`
	}
	results, err := os.ReadFile("testdata/reference/results.json")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(results, &reference))
	require.Equal(t, "cedar-policy 4.13.0", reference.Reference)

	cases, err := os.ReadFile("testdata/reference/cases.json")
	require.NoError(t, err)
	var inputs struct {
		Policies map[string]string `json:"policies"`
		Cases    []struct {
			Name       string          `json:"name"`
			Policy     string          `json:"policy"`
			Action     string          `json:"action"`
			View       string          `json:"view"`
			Attributes json.RawMessage `json:"attributes"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(cases, &inputs))

	require.Equal(t, reference.SchemaSHA256, fmt.Sprintf("%x", sha256.Sum256(schemaBytes)),
		"schema changed: regenerate results.json with the Rust reference")
	require.Equal(t, reference.CasesSHA256, fmt.Sprintf("%x", sha256.Sum256(cases)),
		"cases.json changed: regenerate results.json with the Rust reference")
	require.Len(t, reference.Results, len(inputs.Cases))

	for i, input := range inputs.Cases {
		golden := reference.Results[i]
		t.Run(input.Name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, input.Name, golden.Name)
			policy, ok := inputs.Policies[input.Policy]
			require.True(t, ok, "case names unknown policy %q", input.Policy)

			engine, err := Parse([]byte(policy))
			require.Equal(t, golden.Valid, err == nil, "strict validation differs from the official reference")

			policies, err := cedargo.NewPolicySetFromBytes("fixture", []byte(policy))
			require.NoError(t, err)
			var entities types.EntityMap
			require.NoError(t, json.Unmarshal([]byte(`[{"uid":{"type":"Registry::Principal","id":"fixture"},"attrs":`+
				string(input.Attributes)+`,"parents":[]}]`), &entities))
			uid := types.EntityUID{Type: principalType, ID: "fixture"}
			resource := resourceFor(Action(input.Action))
			if input.View != "" {
				resource.ID = types.String(input.View)
			}
			entities[resource] = types.Entity{UID: resource}

			decision, diagnostic := cedargo.Authorize(policies, entities, cedargo.Request{
				Principal: uid,
				Action:    types.EntityUID{Type: actionType, ID: types.String(input.Action)},
				Resource:  resource,
			})
			want := cedargo.Deny
			if golden.Decision == "Allow" {
				want = cedargo.Allow
			} else {
				require.Equal(t, "Deny", golden.Decision)
			}
			require.Equal(t, want, decision)
			require.Len(t, diagnostic.Errors, golden.Errors)

			if engine == nil {
				// The deliberately ill-typed fixture cannot pass Parse; build
				// the engine directly to prove a raw Allow with diagnostics
				// still fails closed at the package boundary.
				engine = &Engine{policies: policies}
			}
			allowed, err := engine.evaluate(entities[uid], Action(input.Action), resource)
			switch {
			case golden.Errors != 0:
				require.ErrorIs(t, err, ErrEvaluation)
				require.False(t, allowed)
			default:
				require.NoError(t, err)
				require.Equal(t, want == cedargo.Allow, allowed)
			}
		})
	}
}
