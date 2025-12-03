// Package main is the entry point for the ToolHive Registry API server.
package main

import (
	"log/slog"
	"os"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/stacklok/toolhive-registry-server/cmd/thv-registry-api/app"
)

func main() {
	// Setup structured JSON logging with slog
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: false, // Can be enabled for debugging
	})

	logger := slog.New(handler)
	slog.SetDefault(logger)

	// Configure controller-runtime to use same slog handler
	ctrl.SetLogger(logr.FromSlogHandler(handler))

	slog.Info("Starting ToolHive Registry API server")

	if err := app.NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
