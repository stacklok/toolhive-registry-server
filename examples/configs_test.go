package examples

import (
	"embed"
	"io/fs"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

//go:embed config-*.yaml
var configFS embed.FS

func TestConfigFiles(t *testing.T) {
	t.Parallel()

	// Find all config-*.yaml files in the embedded filesystem
	matches, err := fs.Glob(configFS, "config-*.yaml")
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
			data, err := configFS.ReadFile(configPath)
			if err != nil {
				t.Fatalf("Failed to read file: %v", err)
			}

			// Parse YAML into Config type
			var cfg config.Config
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				t.Fatalf("Invalid YAML syntax: %v", err)
			}

			// Validate configuration using the Config type's validation
			if err := cfg.Validate(); err != nil {
				t.Fatalf("Invalid configuration: %v", err)
			}
		})
	}
}
