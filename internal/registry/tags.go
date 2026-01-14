package registry

import upstream "github.com/modelcontextprotocol/registry/pkg/api/v0"

// ExtractTags extracts tags from an upstream server
// It uses the conventions of the Toolhive conversions function in
// github.com/stacklok/toolhive/pkg/registry/converters/toolhive_to_upstream.go
func ExtractTags(server *upstream.ServerJSON) []string {
	if server.Meta == nil || server.Meta.PublisherProvided == nil {
		return []string{}
	}

	// Check for direct "tags" key first
	if tags := extractTagsFromInterface(server.Meta.PublisherProvided["tags"]); len(tags) > 0 {
		return tags
	}

	// Look in the nested ToolHive structure:
	// PublisherProvided["io.github.stacklok"]["<image/url>"]["tags"]
	return extractTagsFromNestedStructure(server.Meta.PublisherProvided)
}

// extractTagsFromInterface extracts string tags from an interface{} that may be []interface{}
func extractTagsFromInterface(tagsValue interface{}) []string {
	tagList, ok := tagsValue.([]interface{})
	if !ok {
		return nil
	}

	tags := make([]string, 0, len(tagList))
	for _, tag := range tagList {
		if tagStr, ok := tag.(string); ok {
			tags = append(tags, tagStr)
		}
	}
	return tags
}

// extractTagsFromNestedStructure extracts tags from the nested ToolHive structure
func extractTagsFromNestedStructure(publisherProvided map[string]interface{}) []string {
	for _, provider := range publisherProvided {
		providerMap, ok := provider.(map[string]interface{})
		if !ok {
			continue
		}
		for _, imageOrURL := range providerMap {
			extensionsMap, ok := imageOrURL.(map[string]interface{})
			if !ok {
				continue
			}
			if tags := extractTagsFromInterface(extensionsMap["tags"]); len(tags) > 0 {
				return tags
			}
		}
	}
	return []string{}
}
