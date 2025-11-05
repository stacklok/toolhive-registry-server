package sources

import (
	"fmt"

	"github.com/stacklok/toolhive-registry-server/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DefaultSourceHandlerFactory is the default implementation of SourceHandlerFactory
type DefaultSourceHandlerFactory struct {
	client client.Client
}

// NewSourceHandlerFactory creates a new source handler factory
func NewSourceHandlerFactory(k8sClient client.Client) SourceHandlerFactory {
	return &DefaultSourceHandlerFactory{
		client: k8sClient,
	}
}

// CreateHandler creates a source handler for the given source type
func (f *DefaultSourceHandlerFactory) CreateHandler(sourceType string) (SourceHandler, error) {
	switch sourceType {
	case config.SourceTypeConfigMap:
		return NewConfigMapSourceHandler(f.client), nil
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
