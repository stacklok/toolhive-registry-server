package validators

import (
	"encoding/json"
	"strings"
	"testing"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSerializeServerMeta(t *testing.T) {
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
				PublisherProvided: map[string]any{},
			},
			expectNil: true,
		},
		{
			name: "with data no size limit",
			meta: &upstreamv0.ServerMeta{
				PublisherProvided: map[string]any{
					"key": "value",
				},
			},
			maxMetaSize: 0,
			expectNil:   false,
		},
		{
			name: "with data within limit",
			meta: &upstreamv0.ServerMeta{
				PublisherProvided: map[string]any{
					"key": "value",
				},
			},
			maxMetaSize: 1000,
			expectNil:   false,
		},
		{
			name: "with data exceeding limit",
			meta: &upstreamv0.ServerMeta{
				PublisherProvided: map[string]any{
					"key": "value",
				},
			},
			maxMetaSize: 5,
			expectError: true,
		},
		{
			name: "with nested data no size limit",
			meta: &upstreamv0.ServerMeta{
				PublisherProvided: map[string]any{
					"nested": map[string]any{
						"key": "value",
					},
				},
			},
			maxMetaSize: 0,
			expectNil:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := SerializeServerMeta(tt.meta, tt.maxMetaSize)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "exceeds maximum allowed size")
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

func TestSerializeServerMeta_ExactBoundary(t *testing.T) {
	t.Parallel()

	// Create metadata and measure its serialized size
	meta := &upstreamv0.ServerMeta{
		PublisherProvided: map[string]any{
			"k": "v",
		},
	}
	serialized, err := json.Marshal(meta.PublisherProvided)
	require.NoError(t, err)
	exactSize := len(serialized)

	// Exactly at the limit should pass (we use > not >=)
	result, err := SerializeServerMeta(meta, exactSize)
	require.NoError(t, err)
	assert.NotNil(t, result)

	// One byte under the limit should fail
	result, err = SerializeServerMeta(meta, exactSize-1)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "exceeds maximum allowed size")
}

func TestSerializeServerMeta_ErrorMessageIncludesSizes(t *testing.T) {
	t.Parallel()

	meta := &upstreamv0.ServerMeta{
		PublisherProvided: map[string]any{
			"data": strings.Repeat("x", 100),
		},
	}

	_, err := SerializeServerMeta(meta, 10)
	require.Error(t, err)
	// Error should include both actual and max sizes
	assert.Contains(t, err.Error(), "10 bytes")
	assert.Contains(t, err.Error(), "exceeds maximum allowed size")
}
