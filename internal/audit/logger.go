package audit

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/stacklok/toolhive-core/audit"
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
		Level: audit.LevelAudit,
		// Render the custom audit level as the string "AUDIT" instead of the
		// default "INFO+2", matching the toolhive operator so operators running
		// both can filter audit streams with a single level pattern.
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.LevelKey {
				if level, ok := a.Value.Any().(slog.Level); ok && level == audit.LevelAudit {
					a.Value = slog.StringValue("AUDIT")
				}
			}
			return a
		},
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
