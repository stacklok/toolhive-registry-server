// Package writer contains the SyncWriter interface and implementations
package writer

import (
	"context"

	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"

	"github.com/stacklok/toolhive-registry-server/internal/service"
)

//go:generate mockgen -destination=mocks/mock_sync_writer.go -package=mocks -source=writer.go SyncWriter

// SyncData bundles server and skill data for a single Store operation.
// Skills is optional: nil means "no skill data provided" (skip skill processing),
// while an empty slice means "no skills exist" (will delete orphaned skills).
type SyncData struct {
	*toolhivetypes.UpstreamRegistry
	Skills []*service.Skill
}

// NewSyncData creates SyncData from an UpstreamRegistry with no skills.
func NewSyncData(reg *toolhivetypes.UpstreamRegistry) *SyncData {
	return &SyncData{UpstreamRegistry: reg}
}

// SyncWriter defines the interface needed to persist synced registry data.
type SyncWriter interface {
	// Store saves registry data (servers and optionally skills) to persistent storage for a specific registry
	Store(ctx context.Context, registryName string, data *SyncData) error
}
