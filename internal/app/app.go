// Package app provides application lifecycle management for the registry server.
package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/stacklok/toolhive/pkg/logger"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// RegistryApp encapsulates all components needed to run the registry API server
// It provides lifecycle management and graceful shutdown capabilities
type RegistryApp struct {
	config     *config.Config
	components *AppComponents
	httpServer *http.Server

	// Lifecycle management
	ctx        context.Context
	cancelFunc context.CancelFunc
}

// Start starts the application components (HTTP server and background sync)
// This method blocks until the HTTP server stops or encounters an error
func (app *RegistryApp) Start() error {
	// Start sync coordinator in background
	go func() {
		if err := app.components.SyncCoordinator.Start(app.ctx); err != nil {
			logger.Errorf("Sync coordinator failed: %v", err)
		}
	}()

	// Start HTTP server (blocks until stopped)
	logger.Infof("Server listening on %s", app.httpServer.Addr)
	if err := app.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("HTTP server failed: %w", err)
	}

	return nil
}

// Stop gracefully stops the application with the given timeout
// It stops the sync coordinator and then shuts down the HTTP server
func (app *RegistryApp) Stop(timeout time.Duration) error {
	logger.Info("Shutting down server...")

	// Stop sync coordinator first
	if err := app.components.SyncCoordinator.Stop(); err != nil {
		logger.Errorf("Failed to stop sync coordinator: %v", err)
	}

	// Cancel the application context
	if app.cancelFunc != nil {
		app.cancelFunc()
	}

	// Graceful HTTP server shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := app.httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server forced to shutdown: %w", err)
	}

	logger.Info("Server shutdown complete")
	return nil
}

// GetConfig returns the application configuration
func (app *RegistryApp) GetConfig() *config.Config {
	return app.config
}

// GetHTTPServer returns the HTTP server (useful for testing to get the actual port)
func (app *RegistryApp) GetHTTPServer() *http.Server {
	return app.httpServer
}
