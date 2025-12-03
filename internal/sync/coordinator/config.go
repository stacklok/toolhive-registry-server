package coordinator

import (
	"log/slog"
	"time"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// getSyncInterval extracts the sync interval from the registry's policy configuration
func getSyncInterval(policy *config.SyncPolicyConfig) time.Duration {
	// Use policy interval if configured
	if policy != nil && policy.Interval != "" {
		if interval, err := time.ParseDuration(policy.Interval); err == nil {
			return interval
		}
		slog.Warn("Invalid sync interval, using default",
			"interval", policy.Interval,
			"default", "1m")
	}

	// Default to 1 minute if no valid interval
	return time.Minute
}
