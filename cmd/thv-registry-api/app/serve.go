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
	"github.com/stacklok/toolhive-registry-server/pkg/sync/coordinator"
	pkgsync "github.com/stacklok/toolhive-registry-server/pkg/sync"

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

	// Ensure data directory exists
	if err := os.MkdirAll("./data", 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	storageManager := sources.NewFileStorageManager("./data")
	statusPersistence := status.NewFileStatusPersistence("./data/status.json")

	// Create sync manager
	syncManager := pkgsync.NewDefaultSyncManager(k8sClient, scheme, sourceHandlerFactory, storageManager)

	// Create and start background sync coordinator
	syncCoordinator := coordinator.New(syncManager, statusPersistence, cfg)

	syncCtx, syncCancel := context.WithCancel(context.Background())
	defer func() {
		if syncCancel != nil {
			syncCancel()
		}
	}()
	go func() {
		if err := syncCoordinator.Start(syncCtx); err != nil {
			logger.Errorf("Sync coordinator failed: %v", err)
		}
	}()

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

	// Stop sync coordinator
	if err := syncCoordinator.Stop(); err != nil {
		logger.Errorf("Failed to stop sync coordinator: %v", err)
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
