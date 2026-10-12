package kubernetes

import (
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	mcpv1beta1 "github.com/stacklok/toolhive/cmd/thv-operator/api/v1beta1"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/operator"
)

// Shared conversion contains no authorization. The legacy controller still
// parses and stamps per-resource claims before invoking its legacy writer.
func extractServer(obj *mcpv1beta1.MCPServer) (*upstreamv0.ServerJSON, error) {
	return operator.Convert(obj)
}
func extractVirtualMCPServer(obj *mcpv1beta1.VirtualMCPServer) (*upstreamv0.ServerJSON, error) {
	return operator.Convert(obj)
}
func extractMCPRemoteProxy(obj *mcpv1beta1.MCPRemoteProxy) (*upstreamv0.ServerJSON, error) {
	return operator.Convert(obj)
}
