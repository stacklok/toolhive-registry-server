package registry

import upstream "github.com/modelcontextprotocol/registry/pkg/api/v0"

// ExtractTags extracts tags from an upstream server's publisher-provided metadata
func ExtractTags(server *upstream.ServerJSON) []string {
	extractedTags := make([]string, 0)
	if server.Meta != nil {
		for _, metadata := range server.Meta.PublisherProvided {
			// Metadata entries may be maps or primitive values; skip non-maps
			metadataMap, ok := metadata.(map[string]any)
			if !ok {
				// Skip non-map values (e.g., strings)
				continue
			}
			for _, metadatas := range metadataMap {
				metadatasMap, ok := metadatas.(map[string]any)
				if !ok {
					// Skip non-map values
					continue
				}
				if tags, ok := metadatasMap["tags"]; ok {
					if tags, ok := tags.([]any); ok {
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
