package registry

import (
	"testing"

	upstream "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stacklok/toolhive/pkg/registry/converters"
	regtypes "github.com/stacklok/toolhive/pkg/registry/types"
	"github.com/stretchr/testify/assert"
)

func TestExtractTags(t *testing.T) {
	t.Parallel()

	serverFromToolhive, _ := converters.ImageMetadataToServerJSON("test-server", &regtypes.ImageMetadata{
		Image: "test/image:latest",
		BaseServerMetadata: regtypes.BaseServerMetadata{
			Name:        "test-server",
			Description: "Test server description",
			Tier:        "Community",
			Status:      "Active",
			Transport:   "stdio",
			Tools:       []string{"test_tool"},
			Tags:        []string{"tag1", "tag2"},
		},
	})

	tests := []struct {
		name         string
		server       *upstream.ServerJSON
		expectedTags []string
	}{
		{
			name: "test with tags",
			server: &upstream.ServerJSON{
				Meta: &upstream.ServerMeta{
					PublisherProvided: map[string]interface{}{
						"provider": map[string]interface{}{
							"image": map[string]interface{}{"tags": []interface{}{"tag1", "tag2"}},
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
					PublisherProvided: map[string]interface{}{},
				},
			},
			expectedTags: []string{},
		},
		{
			name:         "test from converted toolhive tags",
			server:       serverFromToolhive,
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
