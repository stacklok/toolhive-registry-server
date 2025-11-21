package sources

import (
	"fmt"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// defaultRegistryHandlerFactory is the default implementation of RegistryHandlerFactory
type defaultRegistryHandlerFactory struct{}

var _ RegistryHandlerFactory = (*defaultRegistryHandlerFactory)(nil)

// NewRegistryHandlerFactory creates a new registry handler factory
func NewRegistryHandlerFactory() RegistryHandlerFactory {
	return &defaultRegistryHandlerFactory{}
}

// CreateHandler creates a registry handler for the given registry configuration
// The source type is inferred from which field is present (Git/API/File)
func (*defaultRegistryHandlerFactory) CreateHandler(regCfg *config.RegistryConfig) (RegistryHandler, error) {
	if regCfg == nil {
		return nil, fmt.Errorf("registry configuration cannot be nil")
	}

	sourceType := regCfg.GetType()
	if sourceType == "" {
		return nil, fmt.Errorf("unable to determine source type from registry configuration")
	}

	switch sourceType {
	case config.SourceTypeGit:
		return NewGitRegistryHandler(), nil
	case config.SourceTypeAPI:
		return NewAPIRegistryHandler(), nil
	case config.SourceTypeFile:
		return NewFileRegistryHandler(), nil
	default:
		return nil, fmt.Errorf("unsupported source type: %s", sourceType)
	}
}
