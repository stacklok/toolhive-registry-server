package kubernetes

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildEntryClaims(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		baseClaims  map[string]any
		annotations map[string]string
		want        map[string]any // nil means we expect (nil, nil)
		wantErr     bool
	}{
		{
			name:       "no annotation present",
			baseClaims: map[string]any{"org": "acme"},
			annotations: map[string]string{
				"toolhive.stacklok.dev/registry-export": "true",
			},
			want:    nil,
			wantErr: false,
		},
		{
			name:       "empty annotation value",
			baseClaims: map[string]any{"org": "acme"},
			annotations: map[string]string{
				defaultAuthzClaimsAnnotation: "",
			},
			want:    nil,
			wantErr: false,
		},
		{
			name:       "annotation with valid JSON and no base claims",
			baseClaims: nil,
			annotations: map[string]string{
				defaultAuthzClaimsAnnotation: `{"role": "admin"}`,
			},
			want:    map[string]any{"role": "admin"},
			wantErr: false,
		},
		{
			name:       "base claims only with no annotation",
			baseClaims: map[string]any{"org": "acme"},
			annotations: map[string]string{
				"some-other-annotation": "value",
			},
			want:    nil,
			wantErr: false,
		},
		{
			name:       "both base claims and annotation merged",
			baseClaims: map[string]any{"org": "acme", "env": "prod"},
			annotations: map[string]string{
				defaultAuthzClaimsAnnotation: `{"role": "admin", "team": "platform"}`,
			},
			want:    map[string]any{"org": "acme", "env": "prod", "role": "admin", "team": "platform"},
			wantErr: false,
		},
		{
			name:       "annotation with string array values preserved",
			baseClaims: nil,
			annotations: map[string]string{
				defaultAuthzClaimsAnnotation: `{"team": ["eng", "data"]}`,
			},
			want:    map[string]any{"team": []any{"eng", "data"}},
			wantErr: false,
		},
		{
			name:       "invalid JSON in annotation returns error",
			baseClaims: map[string]any{"org": "acme"},
			annotations: map[string]string{
				defaultAuthzClaimsAnnotation: `{not valid json}`,
			},
			want:    nil,
			wantErr: true,
		},
		{
			name:       "annotation overrides base claim key",
			baseClaims: map[string]any{"role": "viewer", "org": "acme"},
			annotations: map[string]string{
				defaultAuthzClaimsAnnotation: `{"role": "admin"}`,
			},
			want:    map[string]any{"role": "admin", "org": "acme"},
			wantErr: false,
		},
		{
			name:       "base claims with multiple keys and annotation adds one key",
			baseClaims: map[string]any{"org": "acme", "env": "prod", "region": "us-east-1"},
			annotations: map[string]string{
				defaultAuthzClaimsAnnotation: `{"team": "platform"}`,
			},
			want:    map[string]any{"org": "acme", "env": "prod", "region": "us-east-1", "team": "platform"},
			wantErr: false,
		},
		{
			name:       "numeric claim value rejected",
			baseClaims: nil,
			annotations: map[string]string{
				defaultAuthzClaimsAnnotation: `{"team": 42}`,
			},
			want:    nil,
			wantErr: true,
		},
		{
			name:       "nested object claim value rejected",
			baseClaims: nil,
			annotations: map[string]string{
				defaultAuthzClaimsAnnotation: `{"team": {"nested": "obj"}}`,
			},
			want:    nil,
			wantErr: true,
		},
		{
			name:       "boolean claim value rejected",
			baseClaims: nil,
			annotations: map[string]string{
				defaultAuthzClaimsAnnotation: `{"active": true}`,
			},
			want:    nil,
			wantErr: true,
		},
		{
			name:       "array with non-string element rejected",
			baseClaims: nil,
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

			got, err := buildEntryClaims(tt.baseClaims, tt.annotations)

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

func TestBuildEntryClaims_BaseClaimsNotMutated(t *testing.T) {
	t.Parallel()

	baseClaims := map[string]any{
		"org":   "acme",
		"teams": []any{"eng", "data"},
	}
	annotations := map[string]string{
		defaultAuthzClaimsAnnotation: `{"role": "admin"}`,
	}

	got, err := buildEntryClaims(baseClaims, annotations)
	require.NoError(t, err)
	require.NotNil(t, got)

	// Verify base claims are not mutated
	assert.Equal(t, map[string]any{
		"org":   "acme",
		"teams": []any{"eng", "data"},
	}, baseClaims)
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
			err := validateClaimValues(tt.claims)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
