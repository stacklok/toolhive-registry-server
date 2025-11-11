package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExampleConfigurations validates that all example configuration files
// can be loaded and pass validation. This ensures that the examples we provide
// to users are always valid and up-to-date with the config schema.
func TestExampleConfigurations(t *testing.T) {
	t.Parallel()
	// Find the examples directory relative to this test file
	examplesDir := filepath.Join("..", "..", "examples")

	// Verify the examples directory exists
	if _, err := os.Stat(examplesDir); os.IsNotExist(err) {
		t.Skipf("Examples directory not found at %s, skipping example validation tests", examplesDir)
		return
	}

	// Find all config-*.yaml files in the examples directory
	entries, err := os.ReadDir(examplesDir)
	require.NoError(t, err, "Failed to read examples directory")

	var exampleFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "config-") && strings.HasSuffix(entry.Name(), ".yaml") {
			t.Helper()
			exampleFiles = append(exampleFiles, entry.Name())
		}
	}

	require.NotEmpty(t, exampleFiles, "No example configuration files found in %s", examplesDir)

	// Test each example configuration file
	loader := NewConfigLoader()

	for _, filename := range exampleFiles {
		t.Run(filename, func(t *testing.T) {
			t.Parallel()
			configPath := filepath.Join(examplesDir, filename)

			// Load the configuration
			cfg, err := loader.LoadConfig(configPath)
			require.NoError(t, err, "Failed to load %s", filename)
			require.NotNil(t, cfg, "Config should not be nil for %s", filename)

			// Validate the configuration
			err = cfg.Validate()
			require.NoError(t, err, "Validation failed for %s", filename)

			// Verify common properties that all examples should have
			assert.NotEmpty(t, cfg.Source.Type, "Source type should be set in %s", filename)
			assert.Contains(t, []string{SourceTypeGit, SourceTypeAPI, SourceTypeFile},
				cfg.Source.Type, "Source type should be valid in %s", filename)

			assert.NotNil(t, cfg.SyncPolicy, "SyncPolicy should be set in %s", filename)
			assert.NotEmpty(t, cfg.SyncPolicy.Interval, "SyncPolicy.Interval should be set in %s", filename)

			// Verify source format is set (if present)
			if cfg.Source.Format != "" {
				assert.Contains(t, []string{SourceFormatToolHive, SourceFormatUpstream},
					cfg.Source.Format, "Source format should be valid in %s", filename)
			}

			// Log successful validation
			t.Logf("✓ %s validated successfully (type=%s, interval=%s)",
				filename, cfg.Source.Type, cfg.SyncPolicy.Interval)
		})
	}
}

// TestExampleConfigurationDetails validates specific details for each example type
func TestExampleConfigurationDetails(t *testing.T) {
	t.Parallel()
	examplesDir := filepath.Join("..", "..", "examples")

	if _, err := os.Stat(examplesDir); os.IsNotExist(err) {
		t.Skipf("Examples directory not found, skipping detailed validation tests")
		return
	}

	loader := NewConfigLoader()

	tests := []struct {
		filename     string
		sourceType   string
		validateFunc func(*testing.T, *Config)
	}{
		{
			filename:   "config-git.yaml",
			sourceType: SourceTypeGit,
			validateFunc: func(t *testing.T, cfg *Config) {
				t.Helper()
				require.NotNil(t, cfg.Source.Git, "Git config should be set")
				assert.NotEmpty(t, cfg.Source.Git.Repository, "Git repository should be set")
				// Branch, tag, or commit should be set (at least one)
				hasRef := cfg.Source.Git.Branch != "" || cfg.Source.Git.Tag != "" || cfg.Source.Git.Commit != ""
				assert.True(t, hasRef, "Git should have at least one ref (branch/tag/commit)")
			},
		},
		{
			filename:   "config-api.yaml",
			sourceType: SourceTypeAPI,
			validateFunc: func(t *testing.T, cfg *Config) {
				t.Helper()
				require.NotNil(t, cfg.Source.API, "API config should be set")
				assert.NotEmpty(t, cfg.Source.API.Endpoint, "API endpoint should be set")
				assert.True(t, strings.HasPrefix(cfg.Source.API.Endpoint, "http://") ||
					strings.HasPrefix(cfg.Source.API.Endpoint, "https://"),
					"API endpoint should be a valid HTTP(S) URL")
			},
		},
		{
			filename:   "config-file.yaml",
			sourceType: SourceTypeFile,
			validateFunc: func(t *testing.T, cfg *Config) {
				t.Helper()
				require.NotNil(t, cfg.Source.File, "File config should be set")
				assert.NotEmpty(t, cfg.Source.File.Path, "File path should be set")
			},
		},
		{
			filename:   "config-complete.yaml",
			sourceType: "", // Can be any type
			validateFunc: func(t *testing.T, cfg *Config) {
				// The complete example should demonstrate all optional features
				t.Helper()
				assert.NotEmpty(t, cfg.GetRegistryName(), "Registry name should be set in complete example")

				// Should have filter configuration to demonstrate filtering
				if cfg.Filter != nil {
					t.Logf("Complete example includes filter configuration")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			t.Parallel()
			configPath := filepath.Join(examplesDir, tt.filename)

			// Check if file exists
			if _, err := os.Stat(configPath); os.IsNotExist(err) {
				t.Skipf("Example file %s not found, skipping", tt.filename)
				return
			}

			// Load and validate
			cfg, err := loader.LoadConfig(configPath)
			require.NoError(t, err, "Failed to load %s", tt.filename)

			err = cfg.Validate()
			require.NoError(t, err, "Validation failed for %s", tt.filename)

			// Check expected source type if specified
			if tt.sourceType != "" {
				assert.Equal(t, tt.sourceType, cfg.Source.Type,
					"Expected %s to have source type %s", tt.filename, tt.sourceType)
			}

			// Run specific validation function
			if tt.validateFunc != nil {
				tt.validateFunc(t, cfg)
			}
		})
	}
}

// TestExampleConfigurationsHaveComments ensures all example files are well-documented
func TestExampleConfigurationsHaveComments(t *testing.T) {
	t.Parallel()
	examplesDir := filepath.Join("..", "..", "examples")

	if _, err := os.Stat(examplesDir); os.IsNotExist(err) {
		t.Skipf("Examples directory not found, skipping comment validation tests")
		return
	}

	entries, err := os.ReadDir(examplesDir)
	require.NoError(t, err)

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "config-") && strings.HasSuffix(entry.Name(), ".yaml") {
			t.Run(entry.Name(), func(t *testing.T) {
				t.Parallel()
				configPath := filepath.Join(examplesDir, entry.Name())
				content, err := os.ReadFile(configPath)
				require.NoError(t, err, "Failed to read %s", entry.Name())

				// Check that the file has comments (starts with #)
				lines := strings.Split(string(content), "\n")
				hasComments := false
				for _, line := range lines {
					if strings.HasPrefix(strings.TrimSpace(line), "#") {
						hasComments = true
						break
					}
				}

				assert.True(t, hasComments, "Example file %s should have comments to help users", entry.Name())
			})
		}
	}
}

// TestExampleREADMEExists verifies that the examples directory has documentation
func TestExampleREADMEExists(t *testing.T) {
	t.Parallel()
	examplesDir := filepath.Join("..", "..", "examples")

	if _, err := os.Stat(examplesDir); os.IsNotExist(err) {
		t.Skipf("Examples directory not found, skipping README test")
		return
	}

	readmePath := filepath.Join(examplesDir, "README.md")
	_, err := os.Stat(readmePath)
	assert.NoError(t, err, "Examples directory should have a README.md file")
}
