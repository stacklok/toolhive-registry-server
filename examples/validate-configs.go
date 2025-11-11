// examples/validate-configs.go
package main

import (
	"fmt"
	"os"

	"github.com/stacklok/toolhive-registry-server/pkg/config"
)

// validate-config is a simple CLI tool to validate a single config file
// Usage: go run validate-config.go <config-file-path>
func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <config-file>\n", os.Args[0])
		os.Exit(1)
	}

	configPath := os.Args[1]

	// Load the config
	loader := config.NewConfigLoader()
	cfg, err := loader.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Validate the config
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Validation error: %v\n", err)
		os.Exit(1)
	}

	// Success - print validation details
	fmt.Printf("✓ Valid configuration\n")
	fmt.Printf("  Registry: %s\n", cfg.GetRegistryName())
	fmt.Printf("  Source type: %s\n", cfg.Source.Type)
	if cfg.Source.Format != "" {
		fmt.Printf("  Format: %s\n", cfg.Source.Format)
	}
	if cfg.SyncPolicy != nil {
		fmt.Printf("  Sync interval: %s\n", cfg.SyncPolicy.Interval)
	}

	os.Exit(0)
}
