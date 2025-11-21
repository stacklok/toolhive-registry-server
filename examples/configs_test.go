package examples

import (
	"embed"
	"io/fs"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
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

			// Validate YAML syntax
			var config map[string]any
			if err := yaml.Unmarshal(data, &config); err != nil {
				t.Fatalf("Invalid YAML syntax: %v", err)
			}

			// Check required fields
			if _, ok := config["source"]; !ok {
				t.Fatal("Missing required field: source")
			}

			source, ok := config["source"].(map[string]any)
			if !ok {
				t.Fatal("source must be a map")
			}

			if _, ok := source["type"]; !ok {
				t.Fatal("Missing required field: source.type")
			}

			if _, ok := source["format"]; !ok {
				t.Fatal("Missing required field: source.format")
			}

			if _, ok := config["syncPolicy"]; !ok {
				t.Fatal("Missing required field: syncPolicy")
			}

			syncPolicy, ok := config["syncPolicy"].(map[string]any)
			if !ok {
				t.Fatal("syncPolicy must be a map")
			}

			if _, ok := syncPolicy["interval"]; !ok {
				t.Fatal("Missing required field: syncPolicy.interval")
			}
		})
	}
}
