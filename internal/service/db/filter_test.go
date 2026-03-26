package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
)

func TestNewDeduplicatingFilterWrongType(t *testing.T) {
	t.Parallel()

	filter := newDeduplicatingFilter()
	_, err := filter(t.Context(), "not-a-helper")
	require.Error(t, err)
}

func TestNewDeduplicatingSkillFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   []any // records passed to the filter; usually []sqlc.ListSkillsRow
		expect  []sqlc.ListSkillsRow
		wantErr bool
	}{
		{
			name:   "empty input returns no records",
			input:  []any{},
			expect: []sqlc.ListSkillsRow{},
		},
		{
			name: "single skill single version returned as-is",
			input: []any{
				sqlc.ListSkillsRow{Name: "skill-a", Version: "1.0.0", Position: 0},
			},
			expect: []sqlc.ListSkillsRow{
				{Name: "skill-a", Version: "1.0.0", Position: 0},
			},
		},
		{
			name: "multiple versions from same source all kept",
			input: []any{
				sqlc.ListSkillsRow{Name: "skill-a", Version: "1.0.0", Position: 2},
				sqlc.ListSkillsRow{Name: "skill-a", Version: "2.0.0", Position: 2},
				sqlc.ListSkillsRow{Name: "skill-a", Version: "3.0.0", Position: 2},
			},
			expect: []sqlc.ListSkillsRow{
				{Name: "skill-a", Version: "1.0.0", Position: 2},
				{Name: "skill-a", Version: "2.0.0", Position: 2},
				{Name: "skill-a", Version: "3.0.0", Position: 2},
			},
		},
		{
			name: "same name from two sources keeps only first-seen position",
			input: []any{
				sqlc.ListSkillsRow{Name: "skill-a", Version: "1.0.0", Position: 1},
				sqlc.ListSkillsRow{Name: "skill-a", Version: "2.0.0", Position: 1},
				sqlc.ListSkillsRow{Name: "skill-a", Version: "1.0.0", Position: 5},
				sqlc.ListSkillsRow{Name: "skill-a", Version: "3.0.0", Position: 5},
			},
			expect: []sqlc.ListSkillsRow{
				{Name: "skill-a", Version: "1.0.0", Position: 1},
				{Name: "skill-a", Version: "2.0.0", Position: 1},
			},
		},
		{
			name: "multiple skill names independently resolved",
			input: []any{
				sqlc.ListSkillsRow{Name: "skill-a", Version: "1.0.0", Position: 3},
				sqlc.ListSkillsRow{Name: "skill-a", Version: "2.0.0", Position: 3},
				sqlc.ListSkillsRow{Name: "skill-b", Version: "1.0.0", Position: 3},
				sqlc.ListSkillsRow{Name: "skill-a", Version: "1.0.0", Position: 7},
				sqlc.ListSkillsRow{Name: "skill-b", Version: "1.0.0", Position: 7},
				sqlc.ListSkillsRow{Name: "skill-b", Version: "2.0.0", Position: 7},
			},
			expect: []sqlc.ListSkillsRow{
				{Name: "skill-a", Version: "1.0.0", Position: 3},
				{Name: "skill-a", Version: "2.0.0", Position: 3},
				{Name: "skill-b", Version: "1.0.0", Position: 3},
			},
		},
		{
			name: "same position for different names keeps all",
			input: []any{
				sqlc.ListSkillsRow{Name: "skill-x", Version: "1.0.0", Position: 4},
				sqlc.ListSkillsRow{Name: "skill-y", Version: "1.0.0", Position: 4},
				sqlc.ListSkillsRow{Name: "skill-y", Version: "2.0.0", Position: 4},
			},
			expect: []sqlc.ListSkillsRow{
				{Name: "skill-x", Version: "1.0.0", Position: 4},
				{Name: "skill-y", Version: "1.0.0", Position: 4},
				{Name: "skill-y", Version: "2.0.0", Position: 4},
			},
		},
		{
			name:    "wrong record type returns error",
			input:   []any{"not-a-skill-row"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			filter := newDeduplicatingSkillFilter()
			var got []sqlc.ListSkillsRow
			for _, record := range tt.input {
				keep, err := filter(t.Context(), record)
				if tt.wantErr {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				if keep {
					got = append(got, record.(sqlc.ListSkillsRow))
				}
			}
			if got == nil {
				got = []sqlc.ListSkillsRow{}
			}

			assert.Equal(t, tt.expect, got)
		})
	}
}
