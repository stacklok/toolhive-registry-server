package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stacklok/toolhive/pkg/logger"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	registryapp "github.com/stacklok/toolhive-registry-server/internal/app"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the registry API server",
	Long: `Start the registry API server to serve MCP registry data.

The server requires a configuration file (--config) that specifies:
- Registry name and data source (Git, API, or File)
- Sync policy and filtering rules
- All other operational settings

See examples/ directory for sample configurations.`,
	RunE: runServe,
}

const (
	defaultGracefulTimeout = 30 * time.Second // Kubernetes-friendly shutdown time
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

func runServe(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	// Initialize controller-runtime logger to suppress warnings
	log.SetLogger(zap.New(zap.UseDevMode(false)))

	// Load and validate configuration
	configPath := viper.GetString("config")
	cfg, err := config.LoadConfig(
		config.WithConfigPath(configPath),
	)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	logger.Infof("Loaded configuration from %s (registry: %s, %d registries configured)",
		configPath, cfg.GetRegistryName(), len(cfg.Registries))

	// Build application using the builder pattern
	address := viper.GetString("address")
	app, err := registryapp.NewRegistryApp(
		ctx,
		registryapp.WithConfig(cfg),
		registryapp.WithAddress(address),
	)
	if err != nil {
		return fmt.Errorf("failed to build application: %w", err)
	}

	logger.Infof("Starting registry API server on %s", address)

	// Start application in goroutine
	go func() {
		if err := app.Start(); err != nil {
			logger.Fatalf("Application failed: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	// Graceful shutdown
	return app.Stop(defaultGracefulTimeout)
}
