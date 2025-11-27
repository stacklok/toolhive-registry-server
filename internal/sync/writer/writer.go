// Package writer contains the SyncWriter interface and implementations
package writer

import (
	"context"

	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"
)

// SyncWriter defines the interface needed to persist the list of MCP servers.
type SyncWriter interface {
	// Store saves a UpstreamRegistry instance to persistent storage for a specific registry
	Store(ctx context.Context, registryName string, reg *toolhivetypes.UpstreamRegistry) error
}
