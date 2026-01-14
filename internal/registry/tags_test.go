package registry

import (
	"testing"

	upstream "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stacklok/toolhive/pkg/registry/converters"
	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"
	"github.com/stretchr/testify/assert"
)

func TestExtractTags(t *testing.T) {
	t.Parallel()

	serverFromToolhive, _ := converters.ImageMetadataToServerJSON("test-server", &toolhivetypes.ImageMetadata{
		Image: "test/image:latest",
		BaseServerMetadata: toolhivetypes.BaseServerMetadata{
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
		{
			name:         "server.Meta is nil",
			server:       &upstream.ServerJSON{Meta: nil},
			expectedTags: []string{},
		},
		{
			name: "server.Meta.PublisherProvided is nil",
			server: &upstream.ServerJSON{
				Meta: &upstream.ServerMeta{
					PublisherProvided: nil,
				},
			},
			expectedTags: []string{},
		},
		{
			name: "direct tags in PublisherProvided (upstream MCP registry format)",
			server: &upstream.ServerJSON{
				Meta: &upstream.ServerMeta{
					PublisherProvided: map[string]interface{}{
						"tags": []interface{}{"upstream-tag1", "upstream-tag2", "upstream-tag3"},
					},
				},
			},
			expectedTags: []string{"upstream-tag1", "upstream-tag2", "upstream-tag3"},
		},
		{
			name: "mixed types in PublisherProvided with string values",
			server: &upstream.ServerJSON{
				Meta: &upstream.ServerMeta{
					PublisherProvided: map[string]interface{}{
						"status":      "active",
						"description": "some description",
						"version":     "1.0.0",
					},
				},
			},
			expectedTags: []string{},
		},
		{
			name: "empty tags array",
			server: &upstream.ServerJSON{
				Meta: &upstream.ServerMeta{
					PublisherProvided: map[string]interface{}{
						"provider": map[string]interface{}{
							"image": map[string]interface{}{"tags": []interface{}{}},
						},
					},
				},
			},
			expectedTags: []string{},
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
