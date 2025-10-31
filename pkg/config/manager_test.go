package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validConfigYAML returns a valid configuration YAML string
func validConfigYAML() string {
	return `source:
  type: configmap
  configmap:
    name: test-registry
syncPolicy:
  interval: "30m"
filter:
  tags:
    include: ["production"]
    exclude: ["beta"]`
}

// invalidConfigYAML returns an invalid configuration YAML string (missing required fields)
func invalidConfigYAML() string {
	return `source:
  type: ""
syncPolicy:
  interval: "30m"
filter:
  tags:
    include: []`
}

// TestNewConfigManager tests the creation of a new ConfigManager
func TestNewConfigManager(t *testing.T) {
	tests := []struct {
		name        string
		setupConfig func(t *testing.T) string
		wantErr     bool
		errMsg      string
	}{
		{
			name: "valid_config",
			setupConfig: func(t *testing.T) string {
				tmpDir := t.TempDir()
				configPath := filepath.Join(tmpDir, "config.yaml")
				err := os.WriteFile(configPath, []byte(validConfigYAML()), 0644)
				require.NoError(t, err)
				return configPath
			},
			wantErr: false,
		},
		{
			name: "invalid_config",
			setupConfig: func(t *testing.T) string {
				tmpDir := t.TempDir()
				configPath := filepath.Join(tmpDir, "config.yaml")
				err := os.WriteFile(configPath, []byte(invalidConfigYAML()), 0644)
				require.NoError(t, err)
				return configPath
			},
			wantErr: true,
			errMsg:  "invalid configuration",
		},
		{
			name: "nonexistent_config",
			setupConfig: func(t *testing.T) string {
				tmpDir := t.TempDir()
				return filepath.Join(tmpDir, "nonexistent.yaml")
			},
			wantErr: true,
			errMsg:  "failed to load initial configuration",
		},
		{
			name: "invalid_yaml_syntax",
			setupConfig: func(t *testing.T) string {
				tmpDir := t.TempDir()
				configPath := filepath.Join(tmpDir, "config.yaml")
				err := os.WriteFile(configPath, []byte("invalid: [yaml"), 0644)
				require.NoError(t, err)
				return configPath
			},
			wantErr: true,
			errMsg:  "failed to load initial configuration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := tt.setupConfig(t)

			manager, err := NewConfigManager(configPath)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, manager)

			// Verify we can get the config
			config := manager.GetConfig()
			assert.NotNil(t, config)

			// Clean up
			err = manager.Close()
			assert.NoError(t, err)
		})
	}
}

// TestGetConfig tests thread-safe config retrieval
func TestGetConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configPath, []byte(validConfigYAML()), 0644)
	require.NoError(t, err)

	manager, err := NewConfigManager(configPath)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, manager.Close())
	}()

	// Test basic retrieval
	config := manager.GetConfig()
	require.NotNil(t, config)
	assert.Equal(t, "configmap", config.Source.Type)
	assert.Equal(t, "test-registry", config.Source.ConfigMap.Name)
	assert.Equal(t, "30m", config.SyncPolicy.Interval)

	// Test that returned config is a copy (modifications don't affect stored config)
	config.Source.Type = "modified"
	config2 := manager.GetConfig()
	assert.Equal(t, "configmap", config2.Source.Type, "Config should be a copy")
}

// TestGetConfigConcurrent tests concurrent access to GetConfig
func TestGetConfigConcurrent(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configPath, []byte(validConfigYAML()), 0644)
	require.NoError(t, err)

	manager, err := NewConfigManager(configPath)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, manager.Close())
	}()

	// Run many concurrent reads
	const numReaders = 100
	var wg sync.WaitGroup
	wg.Add(numReaders)

	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			config := manager.GetConfig()
			assert.NotNil(t, config)
			assert.Equal(t, "configmap", config.Source.Type)
		}()
	}

	wg.Wait()
}

// TestReloadConfig tests configuration reloading
func TestReloadConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Write initial config
	err := os.WriteFile(configPath, []byte(validConfigYAML()), 0644)
	require.NoError(t, err)

	manager, err := NewConfigManager(configPath)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, manager.Close())
	}()

	// Verify initial config
	config := manager.GetConfig()
	assert.Equal(t, "test-registry", config.Source.ConfigMap.Name)

	// Update config file
	newConfig := `source:
  type: configmap
  configmap:
    name: updated-registry
syncPolicy:
  interval: "1h"
filter:
  tags:
    include: ["development"]
    exclude: []`
	err = os.WriteFile(configPath, []byte(newConfig), 0644)
	require.NoError(t, err)

	// Reload config
	err = manager.ReloadConfig()
	require.NoError(t, err)

	// Verify updated config
	config = manager.GetConfig()
	assert.Equal(t, "updated-registry", config.Source.ConfigMap.Name)
	assert.Equal(t, "1h", config.SyncPolicy.Interval)
	assert.Equal(t, []string{"development"}, config.Filter.Tags.Include)
}

// TestReloadConfigFailure tests that old config is preserved on reload failure
func TestReloadConfigFailure(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Write initial valid config
	err := os.WriteFile(configPath, []byte(validConfigYAML()), 0644)
	require.NoError(t, err)

	manager, err := NewConfigManager(configPath)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, manager.Close())
	}()

	// Verify initial config
	config := manager.GetConfig()
	assert.Equal(t, "test-registry", config.Source.ConfigMap.Name)

	// Write invalid config
	err = os.WriteFile(configPath, []byte(invalidConfigYAML()), 0644)
	require.NoError(t, err)

	// Attempt to reload (should fail)
	err = manager.ReloadConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid configuration")

	// Verify old config is still active
	config = manager.GetConfig()
	assert.Equal(t, "test-registry", config.Source.ConfigMap.Name, "Old config should be preserved")
}

// TestReloadConfigConcurrentReads tests reloading while reading
func TestReloadConfigConcurrentReads(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Write initial config
	err := os.WriteFile(configPath, []byte(validConfigYAML()), 0644)
	require.NoError(t, err)

	manager, err := NewConfigManager(configPath)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, manager.Close())
	}()

	// Start many concurrent readers
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	const numReaders = 50

	// Continuous readers
	wg.Add(numReaders)
	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					config := manager.GetConfig()
					assert.NotNil(t, config)
					// Config should always be valid
					assert.NotEmpty(t, config.Source.Type)
				}
			}
		}()
	}

	// Perform multiple reloads while readers are active
	for i := 0; i < 10; i++ {
		newConfig := fmt.Sprintf(`source:
  type: configmap
  configmap:
    name: registry-%d
syncPolicy:
  interval: "30m"
filter:
  tags:
    include: ["test"]
    exclude: []`, i)
		err = os.WriteFile(configPath, []byte(newConfig), 0644)
		require.NoError(t, err)

		err = manager.ReloadConfig()
		require.NoError(t, err)

		time.Sleep(50 * time.Millisecond)
	}

	cancel()
	wg.Wait()
}

// TestConfigValidator tests the default validator
func TestConfigValidator(t *testing.T) {
	validator := &defaultValidator{}

	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid_configmap_config",
			config: &Config{
				Source: SourceConfig{
					Type: "configmap",
					ConfigMap: &ConfigMapConfig{
						Name: "test-registry",
					},
				},
				SyncPolicy: SyncPolicyConfig{
					Interval: "30m",
				},
				Filter: FilterConfig{},
			},
			wantErr: false,
		},
		{
			name:    "nil_config",
			config:  nil,
			wantErr: true,
			errMsg:  "config cannot be nil",
		},
		{
			name: "missing_source_type",
			config: &Config{
				Source: SourceConfig{
					Type: "",
				},
				SyncPolicy: SyncPolicyConfig{
					Interval: "30m",
				},
			},
			wantErr: true,
			errMsg:  "source.type is required",
		},
		{
			name: "configmap_missing_config",
			config: &Config{
				Source: SourceConfig{
					Type:      "configmap",
					ConfigMap: nil,
				},
				SyncPolicy: SyncPolicyConfig{
					Interval: "30m",
				},
			},
			wantErr: true,
			errMsg:  "source.configmap is required when type is configmap",
		},
		{
			name: "configmap_missing_name",
			config: &Config{
				Source: SourceConfig{
					Type: "configmap",
					ConfigMap: &ConfigMapConfig{
						Name: "",
					},
				},
				SyncPolicy: SyncPolicyConfig{
					Interval: "30m",
				},
			},
			wantErr: true,
			errMsg:  "source.configmap.name is required",
		},
		{
			name: "missing_sync_interval",
			config: &Config{
				Source: SourceConfig{
					Type: "configmap",
					ConfigMap: &ConfigMapConfig{
						Name: "test",
					},
				},
				SyncPolicy: SyncPolicyConfig{
					Interval: "",
				},
			},
			wantErr: true,
			errMsg:  "syncPolicy.interval is required",
		},
		{
			name: "invalid_sync_interval_format",
			config: &Config{
				Source: SourceConfig{
					Type: "configmap",
					ConfigMap: &ConfigMapConfig{
						Name: "test",
					},
				},
				SyncPolicy: SyncPolicyConfig{
					Interval: "invalid",
				},
			},
			wantErr: true,
			errMsg:  "must be a valid duration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate(tt.config)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			require.NoError(t, err)
		})
	}
}

// TestWatchConfig tests file watching functionality
func TestWatchConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Write initial config
	err := os.WriteFile(configPath, []byte(validConfigYAML()), 0644)
	require.NoError(t, err)

	manager, err := NewConfigManager(configPath)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, manager.Close())
	}()

	// Start watching in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watchErr := make(chan error, 1)
	go func() {
		watchErr <- manager.WatchConfig(ctx)
	}()

	// Wait for watcher to initialize
	time.Sleep(100 * time.Millisecond)

	// Verify initial config
	config := manager.GetConfig()
	assert.Equal(t, "test-registry", config.Source.ConfigMap.Name)

	// Modify the config file
	newConfig := `source:
  type: configmap
  configmap:
    name: watched-registry
syncPolicy:
  interval: "45m"
filter:
  tags:
    include: ["watched"]
    exclude: []`
	err = os.WriteFile(configPath, []byte(newConfig), 0644)
	require.NoError(t, err)

	// Wait for debounce and reload (debounce is 500ms + some processing time)
	time.Sleep(800 * time.Millisecond)

	// Verify config was reloaded
	config = manager.GetConfig()
	assert.Equal(t, "watched-registry", config.Source.ConfigMap.Name)
	assert.Equal(t, "45m", config.SyncPolicy.Interval)

	// Stop watching
	cancel()

	// Wait for watch to stop
	select {
	case err := <-watchErr:
		// Context cancellation is expected
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("WatchConfig did not stop after context cancellation")
	}
}

// TestWatchConfigImmediate tests that config changes are applied immediately (no debouncing)
// This simulates container environments where updates are atomic (e.g., K8s ConfigMaps)
func TestWatchConfigImmediate(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Write initial config
	err := os.WriteFile(configPath, []byte(validConfigYAML()), 0644)
	require.NoError(t, err)

	manager, err := NewConfigManager(configPath)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, manager.Close())
	}()

	// Start watching in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = manager.WatchConfig(ctx)
	}()

	// Wait for watcher to initialize
	time.Sleep(100 * time.Millisecond)

	// Test immediate application of changes
	testCases := []string{"update-1", "update-2", "update-3"}

	for _, name := range testCases {
		newConfig := fmt.Sprintf(`source:
  type: configmap
  configmap:
    name: %s
syncPolicy:
  interval: "30m"
filter:
  tags:
    include: []
    exclude: []`, name)

		err = os.WriteFile(configPath, []byte(newConfig), 0644)
		require.NoError(t, err)

		// In container environments, changes should be applied immediately
		// Small wait for file system events to propagate
		time.Sleep(100 * time.Millisecond)

		config := manager.GetConfig()
		assert.Equal(t, name, config.Source.ConfigMap.Name,
			"Config should be updated immediately to %s", name)
	}

	cancel()
}

// TestWatchConfigInvalidUpdate tests that invalid config updates don't break watching
func TestWatchConfigInvalidUpdate(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Write initial config
	err := os.WriteFile(configPath, []byte(validConfigYAML()), 0644)
	require.NoError(t, err)

	manager, err := NewConfigManager(configPath)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, manager.Close())
	}()

	// Start watching in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = manager.WatchConfig(ctx)
	}()

	// Wait for watcher to initialize
	time.Sleep(100 * time.Millisecond)

	// Get initial config
	initialConfig := manager.GetConfig()
	assert.Equal(t, "test-registry", initialConfig.Source.ConfigMap.Name)

	// Write invalid config
	err = os.WriteFile(configPath, []byte(invalidConfigYAML()), 0644)
	require.NoError(t, err)

	// Wait for debounce and reload attempt
	time.Sleep(800 * time.Millisecond)

	// Config should still be the old valid one
	config := manager.GetConfig()
	assert.Equal(t, "test-registry", config.Source.ConfigMap.Name)

	// Write valid config again
	validConfig := `source:
  type: configmap
  configmap:
    name: recovered-registry
syncPolicy:
  interval: "15m"
filter:
  tags:
    include: []
    exclude: []`
	err = os.WriteFile(configPath, []byte(validConfig), 0644)
	require.NoError(t, err)

	// Wait for reload
	time.Sleep(800 * time.Millisecond)

	// Should have new valid config
	config = manager.GetConfig()
	assert.Equal(t, "recovered-registry", config.Source.ConfigMap.Name)

	cancel()
}

// TestWatchConfigAlreadyWatching tests that we can't start watching twice
func TestWatchConfigAlreadyWatching(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	err := os.WriteFile(configPath, []byte(validConfigYAML()), 0644)
	require.NoError(t, err)

	manager, err := NewConfigManager(configPath)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, manager.Close())
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start first watcher
	go func() {
		_ = manager.WatchConfig(ctx)
	}()

	// Wait for watcher to initialize
	time.Sleep(100 * time.Millisecond)

	// Try to start second watcher
	err = manager.WatchConfig(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	cancel()
}

// TestConfigManagerWithCustomValidator tests using a custom validator
func TestConfigManagerWithCustomValidator(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Write config
	err := os.WriteFile(configPath, []byte(validConfigYAML()), 0644)
	require.NoError(t, err)

	// Custom validator that always fails
	customValidator := &mockValidator{
		validateFunc: func(config *Config) error {
			return fmt.Errorf("custom validation error")
		},
	}

	// Should fail with custom validator
	_, err = NewConfigManager(configPath, WithValidator(customValidator))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "custom validation error")
}

// TestConfigManagerWithCustomLoader tests using a custom loader
func TestConfigManagerWithCustomLoader(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Custom loader that returns a fixed config
	customLoader := &mockLoader{
		loadFunc: func(path string) (*Config, error) {
			return &Config{
				Source: SourceConfig{
					Type: "configmap",
					ConfigMap: &ConfigMapConfig{
						Name: "custom-loader-registry",
					},
				},
				SyncPolicy: SyncPolicyConfig{
					Interval: "1h",
				},
				Filter: FilterConfig{},
			}, nil
		},
	}

	manager, err := NewConfigManager(configPath, WithLoader(customLoader))
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, manager.Close())
	}()

	config := manager.GetConfig()
	assert.Equal(t, "custom-loader-registry", config.Source.ConfigMap.Name)
}

// TestClose tests proper cleanup of resources
func TestClose(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	err := os.WriteFile(configPath, []byte(validConfigYAML()), 0644)
	require.NoError(t, err)

	manager, err := NewConfigManager(configPath)
	require.NoError(t, err)

	// Start watching
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = manager.WatchConfig(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Close should succeed
	err = manager.Close()
	assert.NoError(t, err)

	// Second close should also succeed (idempotent)
	err = manager.Close()
	assert.NoError(t, err)

	cancel()
}

// Mock implementations for testing

type mockValidator struct {
	validateFunc func(*Config) error
}

func (m *mockValidator) Validate(config *Config) error {
	return m.validateFunc(config)
}

type mockLoader struct {
	loadFunc func(string) (*Config, error)
}

func (m *mockLoader) LoadConfig(path string) (*Config, error) {
	return m.loadFunc(path)
}
