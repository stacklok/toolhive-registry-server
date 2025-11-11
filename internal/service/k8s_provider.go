// Package service provides the business logic for the MCP registry API
package service

import (
	"context"
	"fmt"

	thvv1alpha1 "github.com/stacklok/toolhive/cmd/thv-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Label constants for deployed server identification
const (
	// LabelRegistryName is the label key for the registry name
	LabelRegistryName = "toolhive.stacklok.io/registry-name"
	// LabelRegistryNamespace is the label key for the registry namespace
	LabelRegistryNamespace = "toolhive.stacklok.io/registry-namespace"
	// LabelServerRegistryName is the label key for the server's registry name
	LabelServerRegistryName = "toolhive.stacklok.io/server-name"
)

// getDeployedServerLabelSelector returns the label selector string for finding deployed servers
// that match the registry served by this API instance
func (p *K8sDeploymentProvider) getDeployedServerLabelSelector() string {
	// We want pods that:
	// 1. Have registry-name matching our registry
	// 2. Have registry-namespace present (any value)
	return fmt.Sprintf("%s=%s,%s", LabelRegistryName, p.registryName, LabelRegistryNamespace)
}

// K8sDeploymentProvider implements DeploymentProvider using Kubernetes API.
// This implementation queries Kubernetes MCPServer custom resources to find deployed MCP servers.
type K8sDeploymentProvider struct {
	client       client.Client
	registryName string
}

// NewK8sDeploymentProvider creates a new Kubernetes-based deployment provider.
func NewK8sDeploymentProvider(config *rest.Config, registryName string) (*K8sDeploymentProvider, error) {
	scheme := runtime.NewScheme()
	if err := thvv1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add thvv1alpha1 scheme: %w", err)
	}

	c, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create controller-runtime client: %w", err)
	}

	return &K8sDeploymentProvider{
		client:       c,
		registryName: registryName,
	}, nil
}

// ListDeployedServers implements DeploymentProvider.ListDeployedServers.
// It queries Kubernetes MCPServer custom resources across all namespaces to find deployed MCP servers.
func (p *K8sDeploymentProvider) ListDeployedServers(ctx context.Context) ([]*DeployedServer, error) {
	if p.client == nil {
		return []*DeployedServer{}, nil
	}

	// List MCPServer resources across all namespaces using the registry labels
	labelSelector := p.getDeployedServerLabelSelector()
	return p.listDeployedBySelector(ctx, labelSelector)
}

// GetDeployedServer implements DeploymentProvider.GetDeployedServer.
// It finds all deployed servers that have the specified name as their server-registry-name label value.
func (p *K8sDeploymentProvider) GetDeployedServer(ctx context.Context, name string) ([]*DeployedServer, error) {
	if p.client == nil {
		return []*DeployedServer{}, nil
	}

	// Create label selector to find MCPServers with the specified server-registry-name
	labelSelector := fmt.Sprintf("%s=%s", LabelServerRegistryName, name)
	return p.listDeployedBySelector(ctx, labelSelector)
}

func (p *K8sDeploymentProvider) listDeployedBySelector(ctx context.Context, labelSelector string) ([]*DeployedServer, error) {
	var mcpServerList thvv1alpha1.MCPServerList

	listOpts := &client.ListOptions{}
	if selector, err := metav1.ParseToLabelSelector(labelSelector); err == nil {
		if labelSel, err := metav1.LabelSelectorAsSelector(selector); err == nil {
			listOpts.LabelSelector = labelSel
		}
	}

	if err := p.client.List(ctx, &mcpServerList, listOpts); err != nil {
		return nil, fmt.Errorf("failed to list MCPServer resources: %w", err)
	}

	servers := []*DeployedServer{}
	for i := range mcpServerList.Items {
		srv := newDeployedServerFromMCP(&mcpServerList.Items[i])
		servers = append(servers, srv)
	}

	return servers, nil

}

func newDeployedServerFromMCP(mcpServer *thvv1alpha1.MCPServer) *DeployedServer {
	srv := &DeployedServer{
		Name:        mcpServer.Name,
		Namespace:   mcpServer.Namespace,
		Status:      string(mcpServer.Status.Phase),
		Image:       mcpServer.Spec.Image,
		Transport:   mcpServer.Spec.Transport,
		EndpointURL: mcpServer.Status.URL,
	}
	srv.Ready = isMCPServerReady(mcpServer)
	return srv
}

// isMCPServerReady checks if an MCPServer is ready by examining its phase and conditions
func isMCPServerReady(mcpServer *thvv1alpha1.MCPServer) bool {
	// If no Ready condition found but phase is Running, consider it ready
	return mcpServer.Status.Phase == thvv1alpha1.MCPServerPhaseRunning
}
