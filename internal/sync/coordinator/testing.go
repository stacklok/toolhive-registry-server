package coordinator

import "time"

// TestingWithPollingInterval returns a coordinator Option that overrides
// the default 2-minute polling interval. This is intended for integration
// tests that need faster sync cycles and should not be used in production.
func TestingWithPollingInterval(d time.Duration) Option {
	return withPollingInterval(d)
}
