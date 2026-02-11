package validators

import (
	"encoding/json"
	"fmt"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// SerializeServerMeta serializes the publisher-provided metadata to JSON bytes for storage.
// maxMetaSize specifies the maximum allowed size in bytes. Set to 0 or negative to disable the size check.
func SerializeServerMeta(meta *upstreamv0.ServerMeta, maxMetaSize int) ([]byte, error) {
	if meta == nil || meta.PublisherProvided == nil || len(meta.PublisherProvided) == 0 {
		return nil, nil
	}

	bytes, err := json.Marshal(meta.PublisherProvided)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize server metadata: %w", err)
	}

	if maxMetaSize > 0 && len(bytes) > maxMetaSize {
		return nil, fmt.Errorf(
			"publisher-provided metadata size %d bytes exceeds maximum allowed size of %d bytes",
			len(bytes), maxMetaSize)
	}

	return bytes, nil
}
