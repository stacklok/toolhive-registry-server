// Package main is the entry point for the ToolHive Registry API server.
package main

import (
	"log/slog"
	"os"
	"strings"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/stacklok/toolhive-registry-server/cmd/thv-registry-api/app"
)

// getLogLevel parses the LOG_LEVEL environment variable and returns the corresponding slog.Level.
// Defaults to slog.LevelInfo if LOG_LEVEL is not set or invalid.
func getLogLevel() slog.Level {
	levelStr := os.Getenv("LOG_LEVEL")
	switch strings.ToLower(levelStr) {
	case "debug":
		return slog.LevelDebug
	case "info", "":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		slog.Warn("Invalid LOG_LEVEL, using INFO", "value", levelStr)
		return slog.LevelInfo
	}
}

func main() {
	// Setup structured JSON logging with slog
	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level:     getLogLevel(),
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
