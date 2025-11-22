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

			// Skip database-only configs that don't have registries
			if _, ok := config["database"]; ok && config["registries"] == nil {
				return // Database-only configs are valid
			}

			// Check required fields for registry-based configs
			if _, ok := config["registries"]; !ok {
				t.Fatal("Missing required field: registries")
			}

			registries, ok := config["registries"].([]any)
			if !ok {
				t.Fatal("registries must be an array")
			}

			if len(registries) == 0 {
				t.Fatal("registries array must not be empty")
			}

			// Validate first registry entry
			registry, ok := registries[0].(map[string]any)
			if !ok {
				t.Fatal("registry entry must be a map")
			}

			if _, ok := registry["format"]; !ok {
				t.Fatal("Missing required field: registries[0].format")
			}

			// Check that at least one source type is defined (git, api, or file)
			hasSourceType := false
			for _, sourceType := range []string{"git", "api", "file"} {
				if _, ok := registry[sourceType]; ok {
					hasSourceType = true
					break
				}
			}

			if !hasSourceType {
				t.Fatal("Registry must have at least one source type (git, api, or file)")
			}
		})
	}
}
