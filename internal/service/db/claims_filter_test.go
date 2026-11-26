package database

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stacklok/toolhive-registry-server/internal/claims"
)

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
			// Read-path visibility is OR-within-array (auth.md §3): sharing
			// any one value with a multi-value record claim grants visibility.
			name:       "record list overlaps caller list keeps record",
			callerJSON: []byte(`{"roles":["a"]}`),
			recordJSON: []byte(`{"roles":["a","b"]}`),
			want:       true,
		},
		{
			// Issue #843 canonical case: a source tagged groups:[engineering,
			// devops] must be visible to a caller in only one of those groups.
			name:       "record multi-value group visible to caller in one group",
			callerJSON: []byte(`{"groups":["engineering"]}`),
			recordJSON: []byte(`{"groups":["engineering","devops"]}`),
			want:       true,
		},
		{
			name:       "record list no overlap with caller list drops record",
			callerJSON: []byte(`{"groups":["sales"]}`),
			recordJSON: []byte(`{"groups":["engineering","devops"]}`),
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

			got := claims.VisibleJSON(tt.callerJSON, tt.recordJSON)
			assert.Equal(t, tt.want, got)
		})
	}
}
