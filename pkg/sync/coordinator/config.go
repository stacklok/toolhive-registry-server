package coordinator

import (
	"time"

	"github.com/stacklok/toolhive/pkg/logger"

	"github.com/stacklok/toolhive-registry-server/pkg/config"
)

// getSyncInterval extracts the sync interval from the policy configuration
func getSyncInterval(policy *config.SyncPolicyConfig) time.Duration {
	// Use policy interval if configured
	if policy != nil && policy.Interval != "" {
		if interval, err := time.ParseDuration(policy.Interval); err == nil {
			return interval
		}
		logger.Warnf("Invalid sync interval '%s', using default: 1m", policy.Interval)
	}

	// Default to 1 minute if no valid interval
	return time.Minute
}
