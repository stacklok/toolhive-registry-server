package kubernetes

import (
	"encoding/json"
	"fmt"
	"testing"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	mcpv1alpha1 "github.com/stacklok/toolhive/cmd/thv-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stacklok/toolhive-registry-server/internal/db"
)

func TestParseEntryClaims(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		annotations map[string]string
		want        map[string]any // nil means we expect (nil, nil)
		wantErr     bool
	}{
		{
			name: "no annotation present",
			annotations: map[string]string{
				"toolhive.stacklok.dev/registry-export": "true",
			},
			want:    nil,
			wantErr: false,
		},
		{
			name: "empty annotation value",
			annotations: map[string]string{
				defaultAuthzClaimsAnnotation: "",
			},
			want:    nil,
			wantErr: false,
		},
		{
			name: "valid JSON annotation",
			annotations: map[string]string{
				defaultAuthzClaimsAnnotation: `{"role": "admin"}`,
			},
			want:    map[string]any{"role": "admin"},
			wantErr: false,
		},
		{
			name: "multiple claim keys",
			annotations: map[string]string{
				defaultAuthzClaimsAnnotation: `{"org": "acme", "team": "platform"}`,
			},
			want:    map[string]any{"org": "acme", "team": "platform"},
			wantErr: false,
		},
		{
			name: "string array values preserved",
			annotations: map[string]string{
				defaultAuthzClaimsAnnotation: `{"team": ["eng", "data"]}`,
			},
			want:    map[string]any{"team": []any{"eng", "data"}},
			wantErr: false,
		},
		{
			name: "invalid JSON returns error",
			annotations: map[string]string{
				defaultAuthzClaimsAnnotation: `{not valid json}`,
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "numeric claim value rejected",
			annotations: map[string]string{
				defaultAuthzClaimsAnnotation: `{"team": 42}`,
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "nested object claim value rejected",
			annotations: map[string]string{
				defaultAuthzClaimsAnnotation: `{"team": {"nested": "obj"}}`,
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "boolean claim value rejected",
			annotations: map[string]string{
				defaultAuthzClaimsAnnotation: `{"active": true}`,
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "array with non-string element rejected",
			annotations: map[string]string{
				defaultAuthzClaimsAnnotation: `{"team": ["eng", 123]}`,
			},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseEntryClaims(tt.annotations)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, got)
				return
			}

			require.NoError(t, err)

			if tt.want == nil {
				assert.Nil(t, got)
				return
			}

			require.NotNil(t, got)

			var parsed map[string]any
			err = json.Unmarshal(got, &parsed)
			require.NoError(t, err, "returned bytes should be valid JSON")

			assert.Equal(t, tt.want, parsed)
		})
	}
}

func TestValidateClaimValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		claims  map[string]any
		wantErr bool
	}{
		{
			name:    "all string values",
			claims:  map[string]any{"org": "acme", "team": "platform"},
			wantErr: false,
		},
		{
			name:    "string array values",
			claims:  map[string]any{"teams": []any{"eng", "data"}},
			wantErr: false,
		},
		{
			name:    "mixed string and string array",
			claims:  map[string]any{"org": "acme", "teams": []any{"eng"}},
			wantErr: false,
		},
		{
			name:    "empty map",
			claims:  map[string]any{},
			wantErr: false,
		},
		{
			name:    "number value",
			claims:  map[string]any{"count": float64(42)},
			wantErr: true,
		},
		{
			name:    "boolean value",
			claims:  map[string]any{"active": true},
			wantErr: true,
		},
		{
			name:    "nested object",
			claims:  map[string]any{"meta": map[string]any{"k": "v"}},
			wantErr: true,
		},
		{
			name:    "array with non-string",
			claims:  map[string]any{"teams": []any{"eng", float64(1)}},
			wantErr: true,
		},
		{
			name:    "empty array",
			claims:  map[string]any{"teams": []any{}},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := db.ValidateClaimValues(tt.claims)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// createMCPServerObject creates a test MCPServer as a client.Object with the given name and annotations.
func createMCPServerObject(name string, annotations map[string]string) client.Object {
	return &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			Annotations: annotations,
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			Image:     "test/image:latest",
			Transport: "stdio",
		},
	}
}

// requiredAnnotations returns the minimum annotations needed for hasRequiredRegistryAnnotations to pass.
func requiredAnnotations() map[string]string {
	return map[string]string{
		defaultRegistryDescriptionAnnotation: "A test server",
		defaultRegistryURLAnnotation:         "https://example.com/mcp",
	}
}

// withExtra merges additional key-value pairs into a copy of the base annotations map.
func withExtra(base map[string]string, extra map[string]string) map[string]string {
	merged := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range extra {
		merged[k] = v
	}
	return merged
}

func TestProcessResources(t *testing.T) {
	t.Parallel()

	// A simple extractor that returns a ServerJSON with the object's name.
	// If the object name starts with "error-", it returns an error.
	goodExtractor := func(obj client.Object) (*upstreamv0.ServerJSON, error) {
		name := obj.GetName()
		if len(name) > 6 && name[:6] == "error-" {
			return nil, fmt.Errorf("extractor failure for %s", name)
		}
		return &upstreamv0.ServerJSON{
			Name:    name,
			Version: "1.0.0",
		}, nil
	}

	tests := []struct {
		name               string
		items              []client.Object
		wantServerNames    []string                  // expected server names in serverJSONs slice
		wantClaimsKeys     []string                  // keys expected in perEntryClaims
		wantNoClaimsKeys   []string                  // keys that must NOT be in perEntryClaims
		wantClaimsContents map[string]map[string]any // for keys in perEntryClaims, expected parsed JSON
	}{
		{
			name:            "empty items list",
			items:           []client.Object{},
			wantServerNames: nil,
		},
		{
			name: "single item with valid authz-claims annotation",
			items: []client.Object{
				createMCPServerObject("server-a", withExtra(requiredAnnotations(), map[string]string{
					defaultAuthzClaimsAnnotation: `{"role": "admin"}`,
				})),
			},
			wantServerNames: []string{"server-a"},
			wantClaimsKeys:  []string{"server-a"},
			wantClaimsContents: map[string]map[string]any{
				"server-a": {"role": "admin"},
			},
		},
		{
			name: "single item without authz-claims annotation has no claims",
			items: []client.Object{
				createMCPServerObject("server-b", requiredAnnotations()),
			},
			wantServerNames:  []string{"server-b"},
			wantNoClaimsKeys: []string{"server-b"},
		},
		{
			name: "item missing required registry annotations is skipped",
			items: []client.Object{
				createMCPServerObject("server-no-export", map[string]string{
					"some-unrelated": "annotation",
				}),
			},
			wantServerNames:  nil,
			wantNoClaimsKeys: []string{"server-no-export"},
		},
		{
			name: "item with invalid JSON in authz-claims is skipped entirely",
			items: []client.Object{
				createMCPServerObject("server-bad-json", withExtra(requiredAnnotations(), map[string]string{
					defaultAuthzClaimsAnnotation: `{not valid json}`,
				})),
			},
			wantServerNames:  nil,
			wantNoClaimsKeys: []string{"server-bad-json"},
		},
		{
			name: "item with invalid claim value types is skipped entirely",
			items: []client.Object{
				createMCPServerObject("server-bad-types", withExtra(requiredAnnotations(), map[string]string{
					defaultAuthzClaimsAnnotation: `{"count": 42}`,
				})),
			},
			wantServerNames:  nil,
			wantNoClaimsKeys: []string{"server-bad-types"},
		},
		{
			name: "multiple items with mixed annotations",
			items: []client.Object{
				// Valid with claims — gets annotation claims only
				createMCPServerObject("server-with-claims", withExtra(requiredAnnotations(), map[string]string{
					defaultAuthzClaimsAnnotation: `{"team": "platform"}`,
				})),
				// Valid without claims annotation — no claims (private when authz is on)
				createMCPServerObject("server-no-claims", requiredAnnotations()),
				// Invalid JSON in claims (skipped entirely)
				createMCPServerObject("server-invalid", withExtra(requiredAnnotations(), map[string]string{
					defaultAuthzClaimsAnnotation: `{bad}`,
				})),
				// Missing required annotations (skipped entirely)
				createMCPServerObject("server-skipped", map[string]string{
					"irrelevant": "true",
				}),
			},
			wantServerNames:  []string{"server-with-claims", "server-no-claims"},
			wantClaimsKeys:   []string{"server-with-claims"},
			wantNoClaimsKeys: []string{"server-no-claims", "server-invalid", "server-skipped"},
			wantClaimsContents: map[string]map[string]any{
				"server-with-claims": {"team": "platform"},
			},
		},
		{
			name: "extractor returns error causes item to be skipped",
			items: []client.Object{
				createMCPServerObject("error-server", requiredAnnotations()),
			},
			wantServerNames:  nil,
			wantNoClaimsKeys: []string{"error-server"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var serverJSONs []upstreamv0.ServerJSON
			perEntryClaims := make(map[string][]byte)

			serverJSONs = processResources(tt.items, "MCPServer", goodExtractor, serverJSONs, perEntryClaims)

			// Verify serverJSONs count and names
			if tt.wantServerNames == nil {
				assert.Empty(t, serverJSONs, "expected no serverJSONs")
			} else {
				require.Len(t, serverJSONs, len(tt.wantServerNames), "unexpected number of serverJSONs")
				for i, wantName := range tt.wantServerNames {
					assert.Equal(t, wantName, serverJSONs[i].Name, "serverJSON name mismatch at index %d", i)
				}
			}

			// Verify expected claims keys are present with correct contents
			for _, key := range tt.wantClaimsKeys {
				claimsBytes, ok := perEntryClaims[key]
				assert.True(t, ok, "expected perEntryClaims to contain key %q", key)
				if ok && tt.wantClaimsContents != nil {
					if expected, hasExpected := tt.wantClaimsContents[key]; hasExpected {
						var parsed map[string]any
						err := json.Unmarshal(claimsBytes, &parsed)
						require.NoError(t, err, "perEntryClaims[%q] should be valid JSON", key)
						assert.Equal(t, expected, parsed, "claims mismatch for key %q", key)
					}
				}
			}

			// Verify keys that must NOT be in perEntryClaims
			for _, key := range tt.wantNoClaimsKeys {
				_, ok := perEntryClaims[key]
				assert.False(t, ok, "expected perEntryClaims to NOT contain key %q", key)
			}
		})
	}
}
