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
			name:         "wrong record type returns error",
			callerClaims: map[string]any{"sub": "user1"},
			record:       "not-bytes",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			filter := newClaimsFilterWith(tt.callerClaims, extract)

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := checkClaims(tt.callerJSON, tt.recordJSON)
			assert.Equal(t, tt.want, got)
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
