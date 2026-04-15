// Package writer contains the SyncWriter interface and implementations
package writer

import (
	"context"
	"fmt"

	toolhivetypes "github.com/stacklok/toolhive-core/registry/types"
)

//go:generate mockgen -destination=mocks/mock_sync_writer.go -package=mocks -source=writer.go SyncWriter

// SyncWriter defines the interface needed to persist the list of MCP servers.
type SyncWriter interface {
	// Store saves a UpstreamRegistry instance to persistent storage for a specific registry
	Store(ctx context.Context, registryName string, reg *toolhivetypes.UpstreamRegistry, opts ...StoreOption) error
}

// storeOptions holds optional parameters for a Store call.
type storeOptions struct {
	// PerEntryClaims maps server names to their individual claims JSON.
	// When set, entries use these claims instead of the source-level claims.
	// Entries not present in the map fall back to source-level claims.
	PerEntryClaims map[string][]byte
}

// StoreOption is a function that configures storeOptions.
type StoreOption func(*storeOptions) error

// WithPerEntryClaims provides per-entry claims that override source-level claims.
func WithPerEntryClaims(claims map[string][]byte) StoreOption {
	return func(o *storeOptions) error {
		o.PerEntryClaims = claims
		return nil
	}
}

// parseStoreOptions applies all options and returns the resulting config.
func parseStoreOptions(opts []StoreOption) (*storeOptions, error) {
	o := &storeOptions{}
	for _, opt := range opts {
		if err := opt(o); err != nil {
			return nil, fmt.Errorf("invalid store option: %w", err)
		}
	}
	return o, nil
}
