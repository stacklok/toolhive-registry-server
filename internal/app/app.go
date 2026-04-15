// Package app provides application lifecycle management for the registry server.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// RegistryApp encapsulates all components needed to run the registry API server
// It provides lifecycle management and graceful shutdown capabilities
type RegistryApp struct {
	config             *config.Config
	components         *AppComponents
	httpServer         *http.Server
	internalHTTPServer *http.Server

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
			slog.Error("Sync coordinator failed", "error", err)
		}
	}()

	// Start internal HTTP server in background
	go func() {
		slog.Info("Internal server listening", "address", app.internalHTTPServer.Addr)
		if err := app.internalHTTPServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Internal HTTP server failed", "error", err)
		}
	}()

	// Start HTTP server (blocks until stopped)
	slog.Info("Server listening", "address", app.httpServer.Addr)
	if err := app.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("HTTP server failed: %w", err)
	}

	return nil
}

// Stop gracefully stops the application with the given timeout
// It stops the sync coordinator and then shuts down the HTTP server
func (app *RegistryApp) Stop(timeout time.Duration) error {
	slog.Info("Shutting down server")

	// Stop sync coordinator first
	if err := app.components.SyncCoordinator.Stop(); err != nil {
		slog.Error("Failed to stop sync coordinator", "error", err)
	}

	// Cancel the application context
	if app.cancelFunc != nil {
		app.cancelFunc()
	}

	// Graceful HTTP server shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var shutdownErr error
	if err := app.httpServer.Shutdown(shutdownCtx); err != nil {
		shutdownErr = fmt.Errorf("server forced to shutdown: %w", err)
	}

	if err := app.internalHTTPServer.Shutdown(shutdownCtx); err != nil {
		internalErr := fmt.Errorf("internal server forced to shutdown: %w", err)
		if shutdownErr != nil {
			shutdownErr = errors.Join(shutdownErr, internalErr)
		} else {
			shutdownErr = internalErr
		}
	}

	if shutdownErr != nil {
		return shutdownErr
	}

	slog.Info("Server shutdown complete")
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

// GetInternalHTTPServer returns the internal HTTP server (useful for testing to get the actual port)
func (app *RegistryApp) GetInternalHTTPServer() *http.Server {
	return app.internalHTTPServer
}
