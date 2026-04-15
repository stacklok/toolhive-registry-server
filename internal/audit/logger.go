package audit

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// Logger wraps a dedicated *slog.Logger for audit events and manages
// the underlying writer so it can be closed on shutdown.
type Logger struct {
	logger *slog.Logger
	closer io.Closer // non-nil only when writing to a file
}

// NewLogger creates a dedicated audit logger. If logFile is non-empty,
// events are written to that file (created/appended); otherwise they go
// to stdout. The returned Logger must be closed via Close() on shutdown.
func NewLogger(logFile string) (*Logger, error) {
	var w io.Writer
	var closer io.Closer

	if logFile != "" {
		cleanPath := filepath.Clean(logFile)
		//nolint:gosec // path is cleaned; user-configured audit log destination
		f, err := os.OpenFile(cleanPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return nil, fmt.Errorf("failed to open audit log file %q: %w", cleanPath, err)
		}
		w = f
		closer = f
	} else {
		w = os.Stdout
	}

	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		// Use a custom level between Info and Warn so audit events can be
		// filtered independently from regular application logs.
		Level: auditLevel,
	})

	return &Logger{
		logger: slog.New(handler),
		closer: closer,
	}, nil
}

// Slog returns the underlying *slog.Logger for use with AuditEvent.LogTo().
func (l *Logger) Slog() *slog.Logger {
	return l.logger
}

// Close releases any resources held by the logger (e.g., open file handles).
func (l *Logger) Close() error {
	if l.closer != nil {
		return l.closer.Close()
	}
	return nil
}

// auditLevel is a custom slog level between Info (0) and Warn (4).
// This allows log infrastructure to filter audit events independently.
var auditLevel = slog.Level(2) //nolint:gochecknoglobals // package-level log level constant
