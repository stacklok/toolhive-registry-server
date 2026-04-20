package registry

import (
	"testing"

	upstream "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/assert"
)

func TestExtractTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		server       *upstream.ServerJSON
		expectedTags []string
	}{
		{
			name: "test with tags",
			server: &upstream.ServerJSON{
				Meta: &upstream.ServerMeta{
					PublisherProvided: map[string]any{
						"provider": map[string]any{
							"image": map[string]any{"tags": []any{"tag1", "tag2"}},
						},
					},
				},
			},
			expectedTags: []string{"tag1", "tag2"},
		},
		{
			name: "test without tags",
			server: &upstream.ServerJSON{
				Meta: &upstream.ServerMeta{
					PublisherProvided: map[string]any{},
				},
			},
			expectedTags: []string{},
		},
		{
			name: "test with nested metadata tags",
			server: &upstream.ServerJSON{
				Meta: &upstream.ServerMeta{
					PublisherProvided: map[string]any{
						"provider": map[string]any{
							"metadata": map[string]any{
								"tags": []any{"tag1", "tag2"},
							},
						},
					},
				},
			},
			expectedTags: []string{"tag1", "tag2"},
		},
		{
			name: "test with string values in PublisherProvided (issue #299)",
			server: &upstream.ServerJSON{
				Meta: &upstream.ServerMeta{
					PublisherProvided: map[string]any{
						// String value that would cause panic without fix
						"someKey": "someStringValue",
						// Map value with tags (expected structure)
						"provider": map[string]any{
							"metadata": map[string]any{
								"tags": []any{"tag1", "tag2"},
							},
						},
					},
				},
			},
			expectedTags: []string{"tag1", "tag2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tags := ExtractTags(tt.server)
			assert.Equal(t, tt.expectedTags, tags)
		})
	}
}
