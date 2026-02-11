package database

import (
	"testing"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSerializePublisherProvidedMeta(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		meta        *upstreamv0.ServerMeta
		maxMetaSize int
		expectNil   bool
		expectError bool
	}{
		{
			name:      "nil meta",
			meta:      nil,
			expectNil: true,
		},
		{
			name: "nil PublisherProvided",
			meta: &upstreamv0.ServerMeta{
				PublisherProvided: nil,
			},
			expectNil: true,
		},
		{
			name: "empty PublisherProvided",
			meta: &upstreamv0.ServerMeta{
				PublisherProvided: map[string]interface{}{},
			},
			expectNil: true,
		},
		{
			name: "with data within limit",
			meta: &upstreamv0.ServerMeta{
				PublisherProvided: map[string]interface{}{
					"key": "value",
				},
			},
			maxMetaSize: 1000,
			expectNil:   false,
		},
		{
			name: "with data exceeding limit",
			meta: &upstreamv0.ServerMeta{
				PublisherProvided: map[string]interface{}{
					"key": "value",
				},
			},
			maxMetaSize: 5,
			expectError: true,
		},
		{
			name: "with nested data within limit",
			meta: &upstreamv0.ServerMeta{
				PublisherProvided: map[string]interface{}{
					"nested": map[string]interface{}{
						"key": "value",
					},
				},
			},
			maxMetaSize: 65536,
			expectNil:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := serializePublisherProvidedMeta(tt.meta, tt.maxMetaSize)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				if tt.expectNil {
					assert.Nil(t, result)
				} else {
					assert.NotNil(t, result)
					assert.Greater(t, len(result), 0)
				}
			}
		})
	}
}
