package kubernetes

import (
	mcpv1beta1 "github.com/stacklok/toolhive/cmd/thv-operator/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func createTestVirtualMCPServer(name, namespace string, annotations map[string]string) *mcpv1beta1.VirtualMCPServer {
	return &mcpv1beta1.VirtualMCPServer{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Annotations: annotations}}
}
func createTestMCPRemoteProxy(name, namespace string, annotations map[string]string) *mcpv1beta1.MCPRemoteProxy {
	return &mcpv1beta1.MCPRemoteProxy{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Annotations: annotations}}
}
