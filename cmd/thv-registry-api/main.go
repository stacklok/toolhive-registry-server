// Package main is the entry point for the ToolHive Registry API server.
package main

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/go-logr/logr"
	"github.com/spf13/viper"
	"github.com/stacklok/toolhive-core/logging"
	"go.opentelemetry.io/otel/trace"
	ctrl "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/stacklok/toolhive-registry-server/cmd/thv-registry-api/app"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// getLogLevel parses the THV_REGISTRY_LOG_LEVEL environment variable and returns the corresponding slog.Level.
// Falls back to LOG_LEVEL for backward compatibility.
// Defaults to slog.LevelInfo if neither is set or if the value is invalid.
func getLogLevel() slog.Level {
	// Create a Viper instance for application-level config
	v := viper.New()
	v.SetEnvPrefix(config.EnvPrefix)
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Try THV_REGISTRY_LOG_LEVEL first (via Viper with THV_REGISTRY prefix)
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

// traceHandler wraps an slog.Handler to automatically inject OpenTelemetry
// trace_id and span_id into every log record, enabling log-trace correlation.
type traceHandler struct {
	slog.Handler
}

func (h *traceHandler) Handle(ctx context.Context, r slog.Record) error {
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		r.AddAttrs(
			slog.String("trace_id", span.SpanContext().TraceID().String()),
			slog.String("span_id", span.SpanContext().SpanID().String()),
		)
	}
	return h.Handler.Handle(ctx, r)
}

func (h *traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &traceHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *traceHandler) WithGroup(name string) slog.Handler {
	return &traceHandler{Handler: h.Handler.WithGroup(name)}
}

func main() {
	// Setup structured JSON logging using the shared toolhive-core logging package.
	// Use stderr to keep stdout clean for commands that output data (e.g., version --format json).
	baseHandler := logging.NewHandler(logging.WithLevel(getLogLevel()))
	handler := &traceHandler{Handler: baseHandler}
	logger := slog.New(handler)
	slog.SetDefault(logger)

	// Configure controller-runtime to use same slog handler (with trace injection)
	ctrl.SetLogger(logr.FromSlogHandler(handler))

	slog.Info("Starting ToolHive Registry API server")

	if err := app.NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
