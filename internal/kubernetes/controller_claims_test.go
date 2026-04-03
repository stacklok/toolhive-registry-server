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
			name:       "annotation with array values preserved",
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
