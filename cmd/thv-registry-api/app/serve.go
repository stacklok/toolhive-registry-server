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
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stacklok/toolhive-registry-server/pkg/config"
	"github.com/stacklok/toolhive-registry-server/pkg/sources"
	"github.com/stacklok/toolhive-registry-server/pkg/status"
	"github.com/stacklok/toolhive-registry-server/pkg/sync"

	"github.com/stacklok/toolhive-registry-server/internal/api"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	thvk8scli "github.com/stacklok/toolhive/pkg/container/kubernetes"
	"github.com/stacklok/toolhive/pkg/logger"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the registry API server",
	Long: `Start the registry API server to serve MCP registry data.
The server can read registry data from either:
- ConfigMaps using --from-configmap flag (requires Kubernetes API access)
- Local files using --from-file flag (for mounted ConfigMaps)

Both options require --registry-name to specify the registry identifier.
One of --from-configmap or --from-file must be specified.`,
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
	serveCmd.Flags().String("config", "", "Path to configuration file (YAML format)")
	serveCmd.Flags().String("from-configmap", "", "ConfigMap name containing registry data (mutually exclusive with --from-file)")
	serveCmd.Flags().String("from-file", "", "File path to registry.json (mutually exclusive with --from-configmap)")
	serveCmd.Flags().String("registry-name", "", "Registry name identifier (required)")

	err := viper.BindPFlag("address", serveCmd.Flags().Lookup("address"))
	if err != nil {
		logger.Fatalf("Failed to bind address flag: %v", err)
	}
	err = viper.BindPFlag("config", serveCmd.Flags().Lookup("config"))
	if err != nil {
		logger.Fatalf("Failed to bind config flag: %v", err)
	}
	err = viper.BindPFlag("from-configmap", serveCmd.Flags().Lookup("from-configmap"))
	if err != nil {
		logger.Fatalf("Failed to bind from-configmap flag: %v", err)
	}
	err = viper.BindPFlag("from-file", serveCmd.Flags().Lookup("from-file"))
	if err != nil {
		logger.Fatalf("Failed to bind from-file flag: %v", err)
	}
	err = viper.BindPFlag("registry-name", serveCmd.Flags().Lookup("registry-name"))
	if err != nil {
		logger.Fatalf("Failed to bind registry-name flag: %v", err)
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

// buildProviderConfig creates provider configuration based on command-line flags
func buildProviderConfig() (*service.RegistryProviderConfig, error) {
	configMapName := viper.GetString("from-configmap")
	filePath := viper.GetString("from-file")
	registryName := viper.GetString("registry-name")

	// Validate mutual exclusivity
	if configMapName != "" && filePath != "" {
		return nil, fmt.Errorf("--from-configmap and --from-file flags are mutually exclusive")
	}

	// // Require one of the flags
	// if configMapName == "" && filePath == "" {
	// 	return nil, fmt.Errorf("either --from-configmap or --from-file flag is required")
	// }

	// Require registry name
	if registryName == "" {
		return nil, fmt.Errorf("--registry-name flag is required")
	}

	if configMapName != "" {
		config, err := getKubernetesConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes config: %w", err)
		}

		clientset, err := kubernetes.NewForConfig(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
		}

		return &service.RegistryProviderConfig{
			Type: service.RegistryProviderTypeConfigMap,
			ConfigMap: &service.ConfigMapProviderConfig{
				Name:         configMapName,
				Namespace:    thvk8scli.GetCurrentNamespace(),
				Clientset:    clientset,
				RegistryName: registryName,
			},
		}, nil
	}

	return &service.RegistryProviderConfig{
		Type: service.RegistryProviderTypeFile,
		File: &service.FileProviderConfig{
			FilePath:     filePath,
			RegistryName: registryName,
		},
	}, nil
}

func runServe(_ *cobra.Command, _ []string) error {
	ctx := context.Background()
	address := viper.GetString("address")

	logger.Infof("Starting registry API server on %s", address)

	// Load configuration if provided
	var cfg *config.Config
	configPath := viper.GetString("config")
	if configPath != "" {
		// TODO: Validate the path to avoid path traversal issues
		c, err := config.NewConfigLoader().LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("failed to load configuration: %w", err)
		}
		if err := c.Validate(); err != nil {
			return fmt.Errorf("invalid configuration: %w", err)
		}
		cfg = c
		logger.Infof("Loaded configuration from %s", configPath)
	}

	providerConfig, err := buildProviderConfig()
	if err != nil {
		return fmt.Errorf("failed to build provider configuration: %w", err)
	}

	if err := providerConfig.Validate(); err != nil {
		return fmt.Errorf("invalid provider configuration: %w", err)
	}

	factory := service.NewRegistryProviderFactory()
	registryProvider, err := factory.CreateProvider(providerConfig)
	if err != nil {
		return fmt.Errorf("failed to create registry provider: %w", err)
	}

	logger.Infof("Created registry data provider: %s", registryProvider.GetSource())

	var deploymentProvider service.DeploymentProvider
	k8sRestConfig, err := getKubernetesConfig()
	if err != nil {
		return fmt.Errorf("failed to create kubernetes config for deployment provider: %w", err)
	}

	// Use registry name from provider
	registryName := registryProvider.GetRegistryName()

	deploymentProvider, err = service.NewK8sDeploymentProvider(k8sRestConfig, registryName)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes deployment provider: %w", err)
	}
	logger.Infof("Created Kubernetes deployment provider for registry: %s", registryName)

	// Initialize sync manager if configuration is provided
	var syncManager sync.Manager
	var syncStatus *status.SyncStatus
	var statusPersistence status.StatusPersistence
	var syncCtx context.Context
	var syncCancel context.CancelFunc

	if cfg != nil {
		logger.Info("Initializing sync manager for automatic registry synchronization")

		// Create Kubernetes scheme and register core types
		scheme := runtime.NewScheme()

		// Register core Kubernetes API types (ConfigMap, Secret, etc.)
		if err := clientgoscheme.AddToScheme(scheme); err != nil {
			logger.Warnf("Failed to add Kubernetes core types to scheme: %v", err)
			logger.Warn("Sync manager will not be available")
		} else if k8sClient, err := client.New(k8sRestConfig, client.Options{Scheme: scheme}); err != nil {
			logger.Warnf("Failed to create Kubernetes client for sync manager: %v", err)
			logger.Warn("Sync manager will not be available")
		} else {
			// Initialize sync dependencies
			sourceHandlerFactory := sources.NewSourceHandlerFactory(k8sClient)
			storageManager := sources.NewFileStorageManager("./data")
			statusPersistence = status.NewFileStatusPersistence("./data/status.json")

			// Create sync manager
			syncManager = sync.NewDefaultSyncManager(k8sClient, scheme, sourceHandlerFactory, storageManager)

			// Load existing sync status
			syncStatus, err = statusPersistence.LoadStatus(ctx)
			if err != nil {
				logger.Warnf("Failed to load sync status, starting fresh: %v", err)
				syncStatus = &status.SyncStatus{}
			} else if syncStatus.LastSyncTime != nil {
				logger.Infof("Loaded sync status: last sync at %s, %d servers",
					syncStatus.LastSyncTime.Format(time.RFC3339), syncStatus.ServerCount)
			}

			// Start background sync coordinator
			syncCtx, syncCancel = context.WithCancel(context.Background())
			defer func() {
				if syncCancel != nil {
					syncCancel()
				}
			}()
			go runBackgroundSync(syncCtx, syncManager, cfg, syncStatus, statusPersistence)
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

	// Perform initial sync immediately
	performSync(ctx, mgr, cfg, syncStatus, statusPersistence, false)

	// Continue with periodic sync
	for {
		// Check if we should sync
		shouldSync, reason, nextTime := mgr.ShouldSync(ctx, cfg, syncStatus, false)

		logger.Infof("Sync check: shouldSync=%v, reason=%s, nextTime=%v", shouldSync, reason, nextTime)

		if shouldSync {
			performSync(ctx, mgr, cfg, syncStatus, statusPersistence, false)
		}

		// Calculate sleep duration
		sleepDuration := calculateSleepDuration(nextTime, cfg.SyncPolicy)

		select {
		case <-time.After(sleepDuration):
			continue
		case <-ctx.Done():
			logger.Info("Background sync coordinator shutting down")
			return
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
	manual bool,
) {
	// Update status: Syncing
	syncStatus.Phase = status.SyncPhaseSyncing
	now := time.Now()
	syncStatus.LastAttempt = &now
	syncStatus.AttemptCount++
	if err := statusPersistence.SaveStatus(ctx, syncStatus); err != nil {
		logger.Warnf("Failed to save sync status: %v", err)
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
		logger.Infof("Sync completed successfully: %d servers, hash=%s", result.ServerCount, result.Hash[:8])
	}

	// Persist updated status
	if err := statusPersistence.SaveStatus(ctx, syncStatus); err != nil {
		logger.Warnf("Failed to save sync status: %v", err)
	}
}

// calculateSleepDuration determines how long to sleep before the next sync check
func calculateSleepDuration(nextTime *time.Time, policy *config.SyncPolicyConfig) time.Duration {
	if nextTime != nil && !nextTime.IsZero() {
		if duration := time.Until(*nextTime); duration > 0 {
			return duration
		}
	}

	// If no next time specified, use policy interval or default to 1 minute
	if policy != nil && policy.Interval != "" {
		if interval, err := time.ParseDuration(policy.Interval); err == nil {
			return interval
		}
	}

	// Default to 1 minute if no valid interval
	return time.Minute
}
