package app

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/pkg/sync/coordinator"
)

// AppComponents groups all application components
type AppComponents struct {
	// SyncCoordinator manages background synchronization
	SyncCoordinator coordinator.Coordinator

	// RegistryService provides registry business logic
	RegistryService service.RegistryService

	// KubeComponents contains optional Kubernetes resources
	KubeComponents *KubernetesComponents
}

// KubernetesComponents groups Kubernetes-related components
// These are optional and may be nil if Kubernetes is not available
type KubernetesComponents struct {
	// RestConfig is the Kubernetes REST configuration
	RestConfig *rest.Config

	// Client is the controller-runtime Kubernetes client
	Client client.Client

	// Scheme contains registered Kubernetes types
	Scheme *runtime.Scheme
}
