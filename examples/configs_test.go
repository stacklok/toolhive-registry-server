// Package examples provides test configuration files and utilities
// for validating configuration examples.
package examples

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestConfigFiles(t *testing.T) {
	t.Parallel()

	// Find all config-*.yaml files in the embedded filesystem
	matches, err := fs.Glob(ConfigFS, "config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to find config files: %v", err)
	}

	if len(matches) == 0 {
		t.Fatal("No config-*.yaml files found")
	}

	for _, configPath := range matches {
		t.Run(filepath.Base(configPath), func(t *testing.T) {
			t.Parallel()

			// Read the file from embedded filesystem
			data, err := ConfigFS.ReadFile(configPath)
			if err != nil {
				t.Fatalf("Failed to read file: %v", err)
			}

			// Write to temporary file for LoadConfig to read
			tmpFile, err := os.CreateTemp("", "config-*.yaml")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())
			defer tmpFile.Close()

			if _, err := tmpFile.Write(data); err != nil {
				t.Fatalf("Failed to write temp file: %v", err)
			}
			if err := tmpFile.Close(); err != nil {
				t.Fatalf("Failed to close temp file: %v", err)
			}

			// Load and validate configuration using LoadConfig
			cfg, err := config.LoadConfig(config.WithConfigPath(tmpFile.Name()))
			if err != nil {
				t.Fatalf("Failed to load configuration: %v", err)
			}

			// Ensure config is not nil (LoadConfig validates internally)
			if cfg == nil {
				t.Fatal("LoadConfig returned nil config")
			}
		})
	}
}
