package registry

import upstream "github.com/modelcontextprotocol/registry/pkg/api/v0"

// ExtractTags extracts tags from an upstream server
// It uses the conventions of the Toolhive conversions function in
// github.com/stacklok/toolhive/pkg/registry/converters/toolhive_to_upstream.go
func ExtractTags(server *upstream.ServerJSON) []string {
	extractedTags := make([]string, 0)
	if server.Meta != nil {
		for _, metadata := range server.Meta.PublisherProvided {
			// Handle case where metadata might be a string (upstream format)
			// instead of a map (toolhive format)
			metadataMap, ok := metadata.(map[string]interface{})
			if !ok {
				// Skip non-map values (e.g., strings)
				continue
			}
			for _, metadatas := range metadataMap {
				metadatasMap, ok := metadatas.(map[string]interface{})
				if !ok {
					// Skip non-map values
					continue
				}
				if tags, ok := metadatasMap["tags"]; ok {
					if tags, ok := tags.([]interface{}); ok {
						for _, tag := range tags {
							if tagStr, ok := tag.(string); ok {
								extractedTags = append(extractedTags, tagStr)
							}
						}
					}
				}
			}
		}
	}
	return extractedTags
}
