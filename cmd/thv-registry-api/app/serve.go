package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/stacklok/toolhive-registry-server/pkg/config"
	"github.com/stacklok/toolhive-registry-server/pkg/sources"
	"github.com/stacklok/toolhive-registry-server/pkg/status"
	"github.com/stacklok/toolhive-registry-server/pkg/sync"

	"github.com/stacklok/toolhive-registry-server/internal/api"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive/pkg/logger"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the registry API server",
	Long: `Start the registry API server to serve MCP registry data.

The server requires a configuration file (--config) that specifies:
- Registry name and data source (Git, ConfigMap, API, or File)
- Sync policy and filtering rules
- All other operational settings

See examples/ directory for sample configurations.`,
	RunE: runServe,
}

const (
	defaultGracefulTimeout = 30 * time.Second // Kubernetes-friendly shutdown time
	serverRequestTimeout   = 10 * time.Second // Registry API should respond quickly
	serverReadTimeout      = 10 * time.Second // Enough for headers and small requests
	serverWriteTimeout     = 15 * time.Second // Must be > serverRequestTimeout to let middleware handle timeout
	serverIdleTimeout      = 60 * time.Second // Keep connections alive for reuse
)

func init() {
	serveCmd.Flags().String("address", ":8080", "Address to listen on")
	serveCmd.Flags().String("config", "", "Path to configuration file (YAML format, required)")

	err := viper.BindPFlag("address", serveCmd.Flags().Lookup("address"))
	if err != nil {
		logger.Fatalf("Failed to bind address flag: %v", err)
	}
	err = viper.BindPFlag("config", serveCmd.Flags().Lookup("config"))
	if err != nil {
		logger.Fatalf("Failed to bind config flag: %v", err)
	}

	// Mark config as required
	if err := serveCmd.MarkFlagRequired("config"); err != nil {
		logger.Fatalf("Failed to mark config flag as required: %v", err)
	}
}

// getKubernetesConfig returns a Kubernetes REST config
func getKubernetesConfig() (*rest.Config, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err == nil {
		return config, nil
	}

	// Fall back to kubeconfig
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	return kubeConfig.ClientConfig()
}

// createFileProviderForSyncedData creates a file provider that reads from the synced storage location
// All source types (git, configmap, api, file) are synced to ./data/registry.json by the sync manager
func createFileProviderForSyncedData(registryName string) (*service.RegistryProviderConfig, error) {
	return &service.RegistryProviderConfig{
		FilePath:     "./data/registry.json",
		RegistryName: registryName,
	}, nil
}

func runServe(_ *cobra.Command, _ []string) error {
	ctx := context.Background()
	address := viper.GetString("address")

	// Initialize controller-runtime logger to suppress warnings
	log.SetLogger(zap.New(zap.UseDevMode(false)))

	logger.Infof("Starting registry API server on %s", address)

	// Load and validate configuration (now required)
	configPath := viper.GetString("config")
	// TODO: Validate the path to avoid path traversal issues
	cfg, err := config.NewConfigLoader().LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	logger.Infof("Loaded configuration from %s (registry: %s, source: %s)",
		configPath, cfg.GetRegistryName(), cfg.Source.Type)

	// Get Kubernetes config (needed for both sync manager and deployment provider)
	k8sRestConfig, err := getKubernetesConfig()
	if err != nil {
		logger.Warnf("Failed to create kubernetes config: %v", err)
		logger.Warn("ConfigMap source and deployment provider will not be available")
	}

	// Initialize sync manager for automatic registry synchronization
	logger.Info("Initializing sync manager for automatic registry synchronization")

	// Create Kubernetes scheme and register core types
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add Kubernetes core types to scheme: %w", err)
	}

	// Create Kubernetes client (may be nil if k8s not available)
	var k8sClient client.Client
	if k8sRestConfig != nil {
		k8sClient, err = client.New(k8sRestConfig, client.Options{Scheme: scheme})
		if err != nil {
			logger.Warnf("Failed to create Kubernetes client: %v", err)
			logger.Warn("ConfigMap source will not be available")
		}
	}

	// Initialize sync dependencies
	sourceHandlerFactory := sources.NewSourceHandlerFactory(k8sClient)
	storageManager := sources.NewFileStorageManager("./data")
	statusPersistence := status.NewFileStatusPersistence("./data/status.json")

	// Create sync manager
	syncManager := sync.NewDefaultSyncManager(k8sClient, scheme, sourceHandlerFactory, storageManager)

	// Load existing sync status
	syncStatus, err := statusPersistence.LoadStatus(ctx)
	if err != nil {
		logger.Warnf("Failed to load sync status, initializing with defaults: %v", err)
		syncStatus = &status.SyncStatus{
			Phase:   status.SyncPhaseFailed,
			Message: "No previous sync status found",
		}
	} else {
		// If status was left in Syncing state, it means the previous run was interrupted
		// Reset it to Failed so the sync will be triggered
		if syncStatus.Phase == status.SyncPhaseSyncing {
			logger.Warn("Previous sync was interrupted (status=Syncing), resetting to Failed")
			syncStatus.Phase = status.SyncPhaseFailed
			syncStatus.Message = "Previous sync was interrupted"
		}

		if syncStatus.LastSyncTime != nil {
			logger.Infof("Loaded sync status: phase=%s, last sync at %s, %d servers",
				syncStatus.Phase, syncStatus.LastSyncTime.Format(time.RFC3339), syncStatus.ServerCount)
		} else {
			logger.Infof("Loaded sync status: phase=%s, no previous sync", syncStatus.Phase)
		}
	}

	// Start background sync coordinator (handles initial sync too)
	syncCtx, syncCancel := context.WithCancel(context.Background())
	defer func() {
		if syncCancel != nil {
			syncCancel()
		}
	}()
	go runBackgroundSync(syncCtx, syncManager, cfg, syncStatus, statusPersistence)

	// Create file provider that reads from the synced data
	// All sources (git, configmap, api, file) are now synced to ./data/registry.json
	providerConfig, err := createFileProviderForSyncedData(cfg.GetRegistryName())
	if err != nil {
		return fmt.Errorf("failed to create file provider config: %w", err)
	}

	factory := service.NewRegistryProviderFactory()
	registryProvider, err := factory.CreateProvider(providerConfig)
	if err != nil {
		return fmt.Errorf("failed to create registry provider: %w", err)
	}

	logger.Infof("Created registry data provider reading from synced storage: %s", registryProvider.GetSource())

	// Create deployment provider (optional, requires Kubernetes)
	var deploymentProvider service.DeploymentProvider
	if k8sRestConfig != nil {
		deploymentProvider, err = service.NewK8sDeploymentProvider(k8sRestConfig, cfg.GetRegistryName())
		if err != nil {
			logger.Warnf("Failed to create deployment provider: %v", err)
		} else {
			logger.Infof("Created Kubernetes deployment provider for registry: %s", cfg.GetRegistryName())
		}
	}

	// Create the registry service
	svc, err := service.NewService(ctx, registryProvider, deploymentProvider)
	if err != nil {
		return fmt.Errorf("failed to create registry service: %w", err)
	}

	// Create the registry server with middleware
	router := api.NewServer(svc,
		api.WithMiddlewares(
			middleware.RequestID,
			middleware.RealIP,
			middleware.Recoverer,
			middleware.Timeout(serverRequestTimeout),
			api.LoggingMiddleware,
		),
	)

	// Create HTTP server
	server := &http.Server{
		Addr:         address,
		Handler:      router,
		ReadTimeout:  serverReadTimeout,
		WriteTimeout: serverWriteTimeout,
		IdleTimeout:  serverIdleTimeout,
	}

	// Start server in goroutine
	go func() {
		logger.Infof("Server listening on %s", address)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("Shutting down server...")

	// Cancel sync coordinator if running
	if syncCancel != nil {
		logger.Info("Stopping sync coordinator...")
		syncCancel()
	}

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), defaultGracefulTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Errorf("Server forced to shutdown: %v", err)
		return err
	}

	logger.Info("Server shutdown complete")
	return nil
}

// runBackgroundSync coordinates automatic registry synchronization
func runBackgroundSync(
	ctx context.Context,
	mgr sync.Manager,
	cfg *config.Config,
	syncStatus *status.SyncStatus,
	statusPersistence status.StatusPersistence,
) {
	logger.Info("Starting background sync coordinator")

	// Get sync interval from policy
	interval := getSyncInterval(cfg.SyncPolicy)
	logger.Infof("Configured sync interval: %v", interval)

	// Create ticker for periodic sync
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Perform initial sync check
	checkSync(ctx, mgr, cfg, syncStatus, statusPersistence, "initial")

	// Continue with periodic sync
	for {
		select {
		case <-ticker.C:
			checkSync(ctx, mgr, cfg, syncStatus, statusPersistence, "periodic")
		case <-ctx.Done():
			logger.Info("Background sync coordinator shutting down")
			return
		}
	}
}

// checkSync performs a sync check and updates status accordingly
func checkSync(
	ctx context.Context,
	mgr sync.Manager,
	cfg *config.Config,
	syncStatus *status.SyncStatus,
	statusPersistence status.StatusPersistence,
	checkType string,
) {
	// Update LastAttempt to track when we checked
	now := time.Now()
	syncStatus.LastAttempt = &now

	// Check if sync is needed
	shouldSync, reason, _ := mgr.ShouldSync(ctx, cfg, syncStatus, false)
	logger.Infof("%s sync check: shouldSync=%v, reason=%s", checkType, shouldSync, reason)

	if shouldSync {
		performSync(ctx, mgr, cfg, syncStatus, statusPersistence)
	} else {
		updateStatusForSkippedSync(ctx, syncStatus, statusPersistence, reason)
	}
}

// updateStatusForSkippedSync updates the status when a sync check determines sync is not needed
func updateStatusForSkippedSync(
	ctx context.Context,
	syncStatus *status.SyncStatus,
	statusPersistence status.StatusPersistence,
	reason string,
) {
	// Only update if we have a previous successful sync
	// Don't overwrite Failed/Syncing states with "skipped" messages
	if syncStatus.Phase == status.SyncPhaseComplete {
		syncStatus.Message = fmt.Sprintf("Sync skipped: %s", reason)
		if err := statusPersistence.SaveStatus(ctx, syncStatus); err != nil {
			logger.Warnf("Failed to persist skipped sync status: %v", err)
		}
	}
}

// performSync executes a sync operation and updates status
func performSync(
	ctx context.Context,
	mgr sync.Manager,
	cfg *config.Config,
	syncStatus *status.SyncStatus,
	statusPersistence status.StatusPersistence,
) {
	// Ensure status is persisted at the end, whatever the result
	defer func() {
		if err := statusPersistence.SaveStatus(ctx, syncStatus); err != nil {
			logger.Errorf("Failed to persist final sync status: %v", err)
		}
	}()

	// Update status: Syncing
	syncStatus.Phase = status.SyncPhaseSyncing
	now := time.Now()
	syncStatus.LastAttempt = &now
	syncStatus.AttemptCount++

	// Persist the "Syncing" state immediately so it's visible
	if err := statusPersistence.SaveStatus(ctx, syncStatus); err != nil {
		logger.Warnf("Failed to persist syncing status: %v", err)
	}

	logger.Infof("Starting sync operation (attempt %d)", syncStatus.AttemptCount)

	// Perform sync
	_, result, err := mgr.PerformSync(ctx, cfg)

	// Update status based on result
	if err != nil {
		syncStatus.Phase = status.SyncPhaseFailed
		syncStatus.Message = err.Message
		logger.Errorf("Sync failed: %v", err)
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
		logger.Infof("Sync completed successfully: %d servers, hash=%s", result.ServerCount, hashPreview)
	}
}

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
