package sources

import (
	"fmt"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// DefaultSourceHandlerFactory is the default implementation of SourceHandlerFactory
type DefaultSourceHandlerFactory struct{}

// NewSourceHandlerFactory creates a new source handler factory
func NewSourceHandlerFactory() SourceHandlerFactory {
	return &DefaultSourceHandlerFactory{}
}

// CreateHandler creates a source handler for the given source type
func (*DefaultSourceHandlerFactory) CreateHandler(sourceType string) (SourceHandler, error) {
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
