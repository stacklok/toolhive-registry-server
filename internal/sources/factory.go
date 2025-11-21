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

// CreateHandler creates a source handler for the given registry configuration
// The source type is inferred from which field is present (Git/API/File)
func (*defaultSourceHandlerFactory) CreateHandler(regCfg *config.RegistryConfig) (SourceHandler, error) {
	if regCfg == nil {
		return nil, fmt.Errorf("registry configuration cannot be nil")
	}

	sourceType := regCfg.GetType()
	if sourceType == "" {
		return nil, fmt.Errorf("unable to determine source type from registry configuration")
	}

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
