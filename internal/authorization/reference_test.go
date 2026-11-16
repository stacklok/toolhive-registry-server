package authorization

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/cedar-policy/cedar-go"
	"github.com/cedar-policy/cedar-go/types"
	"github.com/stretchr/testify/require"
)

func TestOfficialReferenceParity(t *testing.T) {
	t.Parallel()
	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		return data
	}
	var reference struct {
		Reference      string
		SchemaSHA256   string `json:"schema_sha256"`
		ExamplesSHA256 string `json:"examples_sha256"`
		CasesSHA256    string `json:"cases_sha256"`
		Results        []struct {
			Name, Decision string
			Valid          bool
			Errors         int
		}
	}
	require.NoError(t, json.Unmarshal(read("testdata/reference/results.json"), &reference))
	require.Equal(t, "cedar-policy 4.13.0", reference.Reference)
	var inputs []struct {
		Name, Policy, Action, View string
		Attributes                 json.RawMessage
	}
	cases := read("testdata/reference/cases.json")
	require.NoError(t, json.Unmarshal(cases, &inputs))
	var blocks []string
	for _, block := range strings.Split(string(read("../../docs/registry-cedar-authorization.md")), "```cedar\n")[1:] {
		policy, _, ok := strings.Cut(block, "```")
		require.True(t, ok)
		blocks = append(blocks, policy)
	}
	examples := strings.Join(blocks, "\n")
	require.Equal(t, reference.SchemaSHA256, fmt.Sprintf("%x", sha256.Sum256(schemaBytes)))
	require.Equal(t, reference.ExamplesSHA256, fmt.Sprintf("%x", sha256.Sum256([]byte(examples))))
	require.Equal(t, reference.CasesSHA256, fmt.Sprintf("%x", sha256.Sum256(cases)))
	require.Len(t, reference.Results, len(inputs))
	for i, input := range inputs {
		t.Run(input.Name, func(t *testing.T) {
			t.Parallel()
			golden := reference.Results[i]
			require.Equal(t, input.Name, golden.Name)
			policy := input.Policy
			if policy == "examples" {
				policy = examples
			}
			engine, err := parse([]byte(policy))
			require.Equal(t, golden.Valid, err == nil, "strict validation differs from official reference")
			policies, err := cedar.NewPolicySetFromBytes("fixture", []byte(policy))
			require.NoError(t, err)
			var entities types.EntityMap
			require.NoError(t, json.Unmarshal([]byte(`[{"uid":{"type":"Registry::Principal","id":"fixture"},"attrs":`+string(input.Attributes)+`,"parents":[]},{"uid":{"type":"Registry::Instance","id":"catalog"},"attrs":{},"parents":[]}]`), &entities))
			uid := types.EntityUID{Type: "Registry::Principal", ID: "fixture"}
			resource := testResource(Action(input.Action))
			if input.View != "" {
				resource.ID = types.String(input.View)
			}
			entities[resource] = types.Entity{UID: resource}
			decision, diagnostic := cedar.Authorize(policies, entities, cedar.Request{Principal: uid,
				Action: types.EntityUID{Type: "Registry::Action", ID: types.String(input.Action)}, Resource: resource})
			want := cedar.Deny
			if golden.Decision == "Allow" {
				want = cedar.Allow
			} else {
				require.Equal(t, "Deny", golden.Decision)
			}
			require.Equal(t, want, decision)
			require.Len(t, diagnostic.Errors, golden.Errors)
			if engine == nil {
				// The deliberately ill-typed diagnostic fixture bypasses Load only
				// to prove that even raw Allow+errors fails the application boundary.
				engine = &Engine{policies: policies}
			}
			switch {
			case golden.Errors != 0:
				require.ErrorIs(t, engine.evaluate(entities[uid], Action(input.Action), resource), ErrEvaluation)
			case want == cedar.Deny:
				require.ErrorIs(t, engine.evaluate(entities[uid], Action(input.Action), resource), ErrDenied)
			default:
				require.NoError(t, engine.evaluate(entities[uid], Action(input.Action), resource))
			}
		})
	}
}
