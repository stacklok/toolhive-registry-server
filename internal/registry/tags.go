package registry

import upstream "github.com/modelcontextprotocol/registry/pkg/api/v0"

// ExtractTags extracts tags from an upstream server
// It uses the conventions of the Toolhive conversions function in
// github.com/stacklok/toolhive/pkg/registry/converters/toolhive_to_upstream.go
func ExtractTags(server *upstream.ServerJSON) []string {
	extractedTags := make([]string, 0)
	if server.Meta != nil {
		for _, metadata := range server.Meta.PublisherProvided {
			for _, metadatas := range metadata.(map[string]interface{}) {
				if tags, ok := metadatas.(map[string]interface{})["tags"]; ok {
					if tags, ok := tags.([]interface{}); ok {
						for _, tag := range tags {
							extractedTags = append(extractedTags, tag.(string))
						}
					}
				}
			}
		}
	}
	return extractedTags
}
