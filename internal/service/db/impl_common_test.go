package database

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
)

func TestFindHighestVersion(t *testing.T) {
	t.Parallel()

	id1 := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	id2 := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	id3 := uuid.MustParse("00000000-0000-0000-0000-000000000003")

	tests := []struct {
		name            string
		rows            []sqlc.ListEntryVersionsRow
		expectedID      uuid.UUID
		expectedVersion string
	}{
		{
			name:            "empty slice returns zero values",
			rows:            []sqlc.ListEntryVersionsRow{},
			expectedID:      uuid.Nil,
			expectedVersion: "",
		},
		{
			name: "single element is returned",
			rows: []sqlc.ListEntryVersionsRow{
				{ID: id1, Version: "1.0.0"},
			},
			expectedID:      id1,
			expectedVersion: "1.0.0",
		},
		{
			name: "highest semver wins regardless of input order",
			rows: []sqlc.ListEntryVersionsRow{
				{ID: id1, Version: "1.0.0"},
				{ID: id3, Version: "2.0.0"},
				{ID: id2, Version: "1.1.0"},
			},
			expectedID:      id3,
			expectedVersion: "2.0.0",
		},
		{
			name: "patch version is compared correctly",
			rows: []sqlc.ListEntryVersionsRow{
				{ID: id1, Version: "1.0.9"},
				{ID: id2, Version: "1.0.10"},
			},
			expectedID:      id2,
			expectedVersion: "1.0.10",
		},
		{
			name: "pre-release version is lower than release",
			rows: []sqlc.ListEntryVersionsRow{
				{ID: id1, Version: "1.0.0-alpha"},
				{ID: id2, Version: "1.0.0"},
			},
			expectedID:      id2,
			expectedVersion: "1.0.0",
		},
		{
			name: "non-semver strings fall back to lexicographic comparison",
			rows: []sqlc.ListEntryVersionsRow{
				{ID: id1, Version: "b"},
				{ID: id2, Version: "a"},
			},
			expectedID:      id1,
			expectedVersion: "b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotID, gotVersion := findHighestVersion(tt.rows)
			require.Equal(t, tt.expectedID, gotID)
			require.Equal(t, tt.expectedVersion, gotVersion)
		})
	}
}
