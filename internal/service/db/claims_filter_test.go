package database

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClaimsFilterWith(t *testing.T) {
	t.Parallel()

	// extract is a simple stand-in that treats the record as []byte directly.
	extract := func(record any) ([]byte, bool) {
		b, ok := record.([]byte)
		return b, ok
	}

	tests := []struct {
		name         string
		callerClaims map[string]any
		record       any
		wantKeep     bool
		wantErr      bool
		wantNilFn    bool
	}{
		{
			name:         "nil caller claims returns nil filter",
			callerClaims: nil,
			wantNilFn:    true,
		},
		{
			name:         "empty caller claims returns nil filter",
			callerClaims: map[string]any{},
			wantNilFn:    true,
		},
		{
			name:         "record with nil claims is dropped",
			callerClaims: map[string]any{"sub": "user1"},
			record:       []byte(nil),
			wantKeep:     false,
		},
		{
			name:         "record with empty claims is dropped",
			callerClaims: map[string]any{"sub": "user1"},
			record:       []byte{},
			wantKeep:     false,
		},
		{
			name:         "matching claims keeps record",
			callerClaims: map[string]any{"sub": "user1"},
			record:       mustMarshal(t, map[string]any{"sub": "user1"}),
			wantKeep:     true,
		},
		{
			name:         "non-matching claims drops record",
			callerClaims: map[string]any{"sub": "user1"},
			record:       mustMarshal(t, map[string]any{"sub": "user2"}),
			wantKeep:     false,
		},
		{
			name:         "caller superset of record claims keeps record",
			callerClaims: map[string]any{"sub": "user1", "org": "acme"},
			record:       mustMarshal(t, map[string]any{"sub": "user1"}),
			wantKeep:     true,
		},
		{
			name:         "wrong record type returns error",
			callerClaims: map[string]any{"sub": "user1"},
			record:       "not-bytes",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := &dbService{}
			filter := svc.newClaimsFilterWith(t.Context(), tt.callerClaims, extract)

			if tt.wantNilFn {
				assert.Nil(t, filter)
				return
			}

			require.NotNil(t, filter)
			keep, err := filter(t.Context(), tt.record)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantKeep, keep)
		})
	}
}

func TestCheckClaims(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		callerJSON []byte
		recordJSON []byte
		want       bool
	}{
		{
			name:       "both nil returns false",
			callerJSON: nil,
			recordJSON: nil,
			want:       false,
		},
		{
			name:       "caller nil record non-nil returns false",
			callerJSON: nil,
			recordJSON: []byte(`{"sub":"user1"}`),
			want:       false,
		},
		{
			name:       "caller non-nil record nil returns false",
			callerJSON: []byte(`{"sub":"user1"}`),
			recordJSON: nil,
			want:       false,
		},
		{
			name:       "both empty returns false",
			callerJSON: []byte{},
			recordJSON: []byte{},
			want:       false,
		},
		{
			name:       "matching JSON returns true",
			callerJSON: []byte(`{"sub":"user1"}`),
			recordJSON: []byte(`{"sub":"user1"}`),
			want:       true,
		},
		{
			name:       "non-matching JSON returns false",
			callerJSON: []byte(`{"sub":"user1"}`),
			recordJSON: []byte(`{"sub":"user2"}`),
			want:       false,
		},
		{
			name:       "invalid caller JSON returns false",
			callerJSON: []byte(`{invalid`),
			recordJSON: []byte(`{"sub":"user1"}`),
			want:       false,
		},
		{
			name:       "invalid record JSON returns false",
			callerJSON: []byte(`{"sub":"user1"}`),
			recordJSON: []byte(`{invalid`),
			want:       false,
		},
		{
			name:       "caller superset keeps record",
			callerJSON: []byte(`{"sub":"u1","org":"acme"}`),
			recordJSON: []byte(`{"sub":"u1"}`),
			want:       true,
		},
		{
			name:       "caller missing required key drops record",
			callerJSON: []byte(`{"org":"acme"}`),
			recordJSON: []byte(`{"sub":"u1"}`),
			want:       false,
		},
		{
			name:       "record list subset of caller list",
			callerJSON: []byte(`{"roles":["a","b","c"]}`),
			recordJSON: []byte(`{"roles":["a","b"]}`),
			want:       true,
		},
		{
			name:       "record list not subset of caller list drops record",
			callerJSON: []byte(`{"roles":["a"]}`),
			recordJSON: []byte(`{"roles":["a","b"]}`),
			want:       false,
		},
		{
			name:       "record string caller list containing it",
			callerJSON: []byte(`{"sub":["u1","u2"]}`),
			recordJSON: []byte(`{"sub":"u1"}`),
			want:       true,
		},
		{
			name:       "record string caller list not containing it drops record",
			callerJSON: []byte(`{"sub":["u2"]}`),
			recordJSON: []byte(`{"sub":"u1"}`),
			want:       false,
		},
		// Default-deny on unsupported record value types (auth.md §4).
		// ValidateClaimValues blocks these at the API edge, but synced
		// sources or direct DB writes could persist them — claimsContain
		// must fail closed rather than treat them as vacuously satisfied.
		{
			name:       "record null value drops record",
			callerJSON: []byte(`{"team":"platform"}`),
			recordJSON: []byte(`{"team":null}`),
			want:       false,
		},
		{
			name:       "record number value drops record",
			callerJSON: []byte(`{"team":"platform"}`),
			recordJSON: []byte(`{"team":42}`),
			want:       false,
		},
		{
			name:       "record nested object value drops record",
			callerJSON: []byte(`{"team":"platform"}`),
			recordJSON: []byte(`{"team":{"nested":"x"}}`),
			want:       false,
		},
		{
			name:       "record mixed-type array drops record",
			callerJSON: []byte(`{"team":["platform","ops"]}`),
			recordJSON: []byte(`{"team":["platform",42]}`),
			want:       false,
		},
		{
			name:       "record boolean value drops record",
			callerJSON: []byte(`{"team":"platform"}`),
			recordJSON: []byte(`{"team":true}`),
			want:       false,
		},
		{
			name:       "record empty array vacuously satisfied (intentional)",
			callerJSON: []byte(`{"team":"platform"}`),
			recordJSON: []byte(`{"team":[]}`),
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := checkClaims(tt.callerJSON, tt.recordJSON)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsValidClaimValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		v    any
		want bool
	}{
		{name: "string", v: "platform", want: true},
		{name: "empty string", v: "", want: true},
		{name: "[]string", v: []string{"a", "b"}, want: true},
		{name: "empty []string", v: []string{}, want: true},
		{name: "[]any of strings", v: []any{"a", "b"}, want: true},
		{name: "empty []any", v: []any{}, want: true},
		{name: "nil", v: nil, want: false},
		{name: "int", v: 42, want: false},
		{name: "float", v: 3.14, want: false},
		{name: "bool", v: true, want: false},
		{name: "nested map", v: map[string]any{"k": "v"}, want: false},
		{name: "[]any with int", v: []any{"a", 42}, want: false},
		{name: "[]any with nil", v: []any{"a", nil}, want: false},
		{name: "[]any with nested map", v: []any{"a", map[string]any{}}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isValidClaimValue(tt.v))
		})
	}
}

func TestMarshalClaims(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		claims map[string]any
		isNil  bool
	}{
		{
			name:   "nil map returns nil",
			claims: nil,
			isNil:  true,
		},
		{
			name:   "empty map returns nil",
			claims: map[string]any{},
			isNil:  true,
		},
		{
			name:   "non-empty map returns non-nil",
			claims: map[string]any{"sub": "user1"},
			isNil:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := marshalClaims(tt.claims)
			if tt.isNil {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
			}
		})
	}
}

// mustMarshal is a test helper that marshals v to JSON or fails the test.
func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}
