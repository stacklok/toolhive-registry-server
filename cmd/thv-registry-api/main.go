// Package main is the entry point for the ToolHive Registry API server.
package main

import (
	"log/slog"
	"os"
	"strings"

	"github.com/go-logr/logr"
	"github.com/spf13/viper"
	ctrl "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/stacklok/toolhive-registry-server/cmd/thv-registry-api/app"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// getLogLevel parses the THV_LOG_LEVEL environment variable and returns the corresponding slog.Level.
// Falls back to LOG_LEVEL for backward compatibility.
// Defaults to slog.LevelInfo if neither is set or if the value is invalid.
func getLogLevel() slog.Level {
	// Create a Viper instance for application-level config
	v := viper.New()
	v.SetEnvPrefix(config.EnvPrefix)
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Try THV_LOG_LEVEL first (via Viper with THV prefix)
	levelStr := v.GetString("LOG_LEVEL")

	// Fall back to LOG_LEVEL without prefix for backward compatibility
	if levelStr == "" {
		levelStr = os.Getenv("LOG_LEVEL")
	}

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
