package coordinator

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/otel"
	"github.com/stacklok/toolhive-registry-server/internal/sources"
	"github.com/stacklok/toolhive-registry-server/internal/status"
	pkgsync "github.com/stacklok/toolhive-registry-server/internal/sync"
	"github.com/stacklok/toolhive-registry-server/internal/sync/state"
	"github.com/stacklok/toolhive-registry-server/internal/telemetry"
)

const (
	// CoordinatorTracerName is the tracer name for sync coordinator operations
	CoordinatorTracerName = "github.com/stacklok/toolhive-registry-server/sync/coordinator"

	// basePollingInterval is the base interval at which the coordinator checks for sync jobs
	basePollingInterval = 2 * time.Minute
	// pollingJitter is the maximum random offset (±30 seconds) applied to the polling interval
	pollingJitter = 30 * time.Second
)

// Coordinator manages background synchronization scheduling and execution for multiple registries
type Coordinator interface {
	// Start begins background sync coordination for all registries
	// Blocks until context is cancelled or an unrecoverable error occurs
	Start(ctx context.Context) error

	// Stop gracefully stops the coordinator and all registry sync loops
	Stop() error
}

// defaultCoordinator is the default implementation of Coordinator
type defaultCoordinator struct {
	manager pkgsync.Manager
	config  *config.Config

	// Lifecycle management
	cancelFunc context.CancelFunc
	done       chan struct{}

	statusSvc state.RegistryStateService

	// Metrics
	syncMetrics     *telemetry.SyncMetrics
	registryMetrics *telemetry.RegistryMetrics

	// Tracing
	tracer trace.Tracer

	// pollingIntervalOverride overrides the default polling interval (for testing)
	pollingIntervalOverride time.Duration
}

// Option is a function that configures the coordinator
type Option func(*defaultCoordinator)

// WithSyncMetrics sets the sync metrics for the coordinator
func WithSyncMetrics(metrics *telemetry.SyncMetrics) Option {
	return func(c *defaultCoordinator) {
		c.syncMetrics = metrics
	}
}

// WithRegistryMetrics sets the registry metrics for the coordinator
func WithRegistryMetrics(metrics *telemetry.RegistryMetrics) Option {
	return func(c *defaultCoordinator) {
		c.registryMetrics = metrics
	}
}

// WithTracer sets the OpenTelemetry tracer for the coordinator.
// If not set, tracing will be disabled (no-op).
func WithTracer(tracer trace.Tracer) Option {
	return func(c *defaultCoordinator) {
		c.tracer = tracer
	}
}

// WithPollingInterval overrides the default polling interval.
// Intended for integration tests that need faster sync cycles.
func WithPollingInterval(d time.Duration) Option {
	return func(c *defaultCoordinator) {
		c.pollingIntervalOverride = d
	}
}

// New creates a new coordinator with injected dependencies
func New(
	manager pkgsync.Manager,
	statusSvc state.RegistryStateService,
	cfg *config.Config,
	opts ...Option,
) Coordinator {
	c := &defaultCoordinator{
		manager:   manager,
		statusSvc: statusSvc,
		config:    cfg,
		done:      make(chan struct{}),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// calculatePollingInterval returns the base polling interval with a random jitter applied.
// The jitter is ±30 seconds to prevent all instances from polling the database simultaneously.
func calculatePollingInterval() time.Duration {
	// Generate a random offset between -pollingJitter and +pollingJitter
	//nolint:gosec // G404: Non-cryptographic randomness is sufficient for polling jitter
	jitterOffset := time.Duration(rand.Int64N(int64(2*pollingJitter))) - pollingJitter
	return basePollingInterval + jitterOffset
}

// Start begins background sync coordination for all registries
func (c *defaultCoordinator) Start(ctx context.Context) error {
	slog.Info("Starting background sync coordinator", "source_count", len(c.config.Sources))

	// Create cancellable context for this coordinator
	coordCtx, cancel := context.WithCancel(ctx)
	c.cancelFunc = cancel
	defer func() {
		close(c.done)
		slog.Info("Background sync coordinator shutting down")
	}()

	// Load or initialize sync status for all registries
	if err := c.statusSvc.Initialize(ctx, c.config); err != nil {
		return fmt.Errorf("failed to initialize registry sync status: %w", err)
	}

	// Calculate polling interval with jitter to prevent thundering herd
	var pollingInterval time.Duration
	if c.pollingIntervalOverride > 0 {
		pollingInterval = c.pollingIntervalOverride
	} else {
		pollingInterval = calculatePollingInterval()
	}
	slog.Info("Configured coordinator sync interval",
		"base_interval", basePollingInterval,
		"actual_interval", pollingInterval)

	// Create ticker for periodic sync checks
	ticker := time.NewTicker(pollingInterval)
	defer ticker.Stop()

	// Perform initial sync check
	c.processNextSyncJob(coordCtx)

	// Run the coordinator loop
	for {
		select {
		case <-ticker.C:
			c.processNextSyncJob(coordCtx)

			// Recalculate interval with new jitter for next iteration
			if c.pollingIntervalOverride > 0 {
				ticker.Reset(c.pollingIntervalOverride)
			} else {
				ticker.Reset(calculatePollingInterval())
			}
		case <-coordCtx.Done():
			slog.Info("Sync coordinator stopping")
			return nil
		}
	}
}

// Stop gracefully stops the coordinator
func (c *defaultCoordinator) Stop() error {
	if c.cancelFunc != nil {
		slog.Info("Stopping sync coordinator")
		c.cancelFunc()
		// Wait for coordinator to finish
		<-c.done
	}
	return nil
}

// processNextSyncJob gets the next job and processes it if available
func (c *defaultCoordinator) processNextSyncJob(ctx context.Context) {
	var prefetched *sources.FetchResult
	// Get the next sync job using the predicate to check if sync is needed
	regCfg, err := c.statusSvc.GetNextSyncJob(
		ctx,
		func(regCfg *config.SourceConfig, syncStatus *status.SyncStatus) bool {
			reason, fetchResult := c.manager.ShouldSync(ctx, regCfg, syncStatus, false)
			if !reason.ShouldSync() {
				slog.Debug("Registry does not need sync",
					"registry", regCfg.Name,
					"reason", reason.String())
				return false
			}
			prefetched = fetchResult
			return true
		},
	)
	if err != nil {
		slog.Error("Error getting next sync job", "error", err)
		return
	}

	// No job available
	if regCfg == nil {
		return
	}

	// Perform the sync
	c.performRegistrySync(ctx, regCfg, prefetched)
}

// performRegistrySync executes the sync operation for a registry
func (c *defaultCoordinator) performRegistrySync(
	ctx context.Context, regCfg *config.SourceConfig, prefetched *sources.FetchResult,
) {
	registryName := regCfg.Name
	startTime := time.Now()

	// Start tracing span for this sync operation
	ctx, span := otel.StartSpan(ctx, c.tracer, "sync.performRegistrySync",
		trace.WithAttributes(
			otel.AttrRegistryName.String(regCfg.Name),
			otel.AttrRegistryType.String(string(regCfg.GetType())),
		),
	)
	defer span.End()

	// Set up the final status update in a defer block to ensure that we always
	// clean up the status of the sync at the end of this function.
	// Set a default error here in case the function is killed by an unexpected error.
	syncStatus := &status.SyncStatus{
		Phase:   status.SyncPhaseFailed,
		Message: fmt.Sprintf("Unexpected failure while syncing registry %s", registryName),
	}
	defer func() {
		if err := c.statusSvc.UpdateSyncStatus(ctx, registryName, syncStatus); err != nil {
			slog.Error("Error updating sync status",
				"registry", registryName,
				"error", err)
		}
	}()

	slog.Info("Starting sync operation", "registry", registryName)

	// Perform sync
	result, syncErr := c.manager.PerformSync(ctx, regCfg, prefetched)

	// Calculate sync duration for metrics and tracing
	syncDuration := time.Since(startTime)

	// Add sync result attributes to span
	span.SetAttributes(
		attribute.Bool("sync.success", syncErr == nil),
		attribute.Float64("sync.duration_seconds", syncDuration.Seconds()),
	)

	// Update status based on result
	now := time.Now()
	if syncErr != nil {
		syncStatus.Phase = status.SyncPhaseFailed
		syncStatus.Message = syncErr.Message
		slog.Error("Sync failed",
			"registry", registryName,
			"error", syncErr.Message)

		// Record error on span
		otel.RecordError(span, fmt.Errorf("%s", syncErr.Message))

		// Record sync failure metric
		if c.syncMetrics != nil {
			c.syncMetrics.RecordSyncDuration(ctx, registryName, syncDuration, false)
		}
	} else {
		syncStatus.Phase = status.SyncPhaseComplete
		syncStatus.Message = "Sync completed successfully"
		syncStatus.LastSyncTime = &now
		syncStatus.LastSyncHash = result.Hash
		syncStatus.ServerCount = result.ServerCount
		syncStatus.AttemptCount = 0
		hashPreview := result.Hash
		if len(hashPreview) > 8 {
			hashPreview = hashPreview[:8]
		}
		slog.Info("Sync completed successfully",
			"registry", registryName,
			"server_count", result.ServerCount,
			"hash", hashPreview)

		// Add server count to span on success
		span.SetAttributes(attribute.Int("sync.server_count", result.ServerCount))

		// Record sync success metric
		if c.syncMetrics != nil {
			c.syncMetrics.RecordSyncDuration(ctx, registryName, syncDuration, true)
		}

		// Record registry server count metric
		if c.registryMetrics != nil {
			c.registryMetrics.RecordServersTotal(ctx, registryName, int64(result.ServerCount))
		}
	}
}
