package sources

import (
	"fmt"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// defaultSourceHandlerFactory is the default implementation of SourceHandlerFactory
type defaultSourceHandlerFactory struct{}

var _ SourceHandlerFactory = (*defaultSourceHandlerFactory)(nil)

// NewSourceHandlerFactory creates a new source handler factory
func NewSourceHandlerFactory() SourceHandlerFactory {
	return &defaultSourceHandlerFactory{}
}

// CreateHandler creates a source handler for the given source type
func (*defaultSourceHandlerFactory) CreateHandler(sourceType string) (SourceHandler, error) {
	switch sourceType {
	case config.SourceTypeGit:
		return NewGitSourceHandler(), nil
	case config.SourceTypeAPI:
		return NewAPISourceHandler(), nil
	case config.SourceTypeFile:
		return NewFileSourceHandler(), nil
	default:
		return nil, fmt.Errorf("unsupported source type: %s", sourceType)
	}
}
