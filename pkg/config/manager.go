package config

import (
	"context"
	"fmt"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/stacklok/toolhive/pkg/logger"
)

// ConfigManager provides thread-safe, read-only configuration management.
// Configuration files are never modified by the application - all updates come from
// external sources (Kubernetes ConfigMaps, Docker volume mounts, orchestration tools).
//
// Design for read-only operation:
// - No file locking needed - we only observe external changes
// - No write coordination required - we never modify files
// - Uses sync.RWMutex optimized for concurrent reads
// - Validates externally-updated configs before applying
// - Preserves last known good configuration on invalid updates
// - Optimized for container environments with atomic file updates
type ConfigManager interface {
	// GetConfig safely retrieves the current configuration
	GetConfig() *Config

	// ReloadConfig reads the latest configuration from disk and applies it if valid.
	// The file is only read, never written. Returns error if the new config is invalid.
	ReloadConfig() error

	// WatchConfig observes the configuration file for external changes.
	// Automatically reloads when the file is updated by external systems.
	// Blocks until context is cancelled.
	WatchConfig(ctx context.Context) error

	// Close releases the file watcher resources
	Close() error
}

// ConfigValidator defines the interface for validating configurations
// This allows custom validation logic to be injected beyond the basic validation
type ConfigValidator interface {
	Validate(config *Config) error
}

// defaultValidator uses the Config's built-in validation
type defaultValidator struct{}

// Validate delegates to the Config's own Validate method
func (v *defaultValidator) Validate(config *Config) error {
	return config.Validate()
}

// configManager is the concrete implementation of ConfigManager
type configManager struct {
	mu         sync.RWMutex      // Protects concurrent access to config
	config     *Config           // Current active configuration
	configPath string            // Path to configuration file
	loader     ConfigLoader      // Loader for reading config files
	validator  ConfigValidator   // Validator for checking config validity
	watcher    *fsnotify.Watcher // File system watcher (nil if not watching)
	watcherMu  sync.Mutex        // Protects watcher field
}

// ConfigManagerOption allows customizing ConfigManager behavior
type ConfigManagerOption func(*configManager)

// WithValidator sets a custom validator for the config manager
func WithValidator(validator ConfigValidator) ConfigManagerOption {
	return func(cm *configManager) {
		cm.validator = validator
	}
}

// WithLoader sets a custom config loader for the config manager
func WithLoader(loader ConfigLoader) ConfigManagerOption {
	return func(cm *configManager) {
		cm.loader = loader
	}
}

// NewConfigManager creates a new ConfigManager with the given configuration file path.
// It loads and validates the initial configuration.
// Returns error if initial load or validation fails.
func NewConfigManager(configPath string, opts ...ConfigManagerOption) (ConfigManager, error) {
	cm := &configManager{
		configPath: configPath,
		loader:     NewConfigLoader(),
		validator:  &defaultValidator{}, // Uses Config.Validate() by default
	}

	// Apply options
	for _, opt := range opts {
		opt(cm)
	}

	// Load initial configuration
	if err := cm.ReloadConfig(); err != nil {
		return nil, fmt.Errorf("failed to load initial configuration: %w", err)
	}

	return cm, nil
}

// GetConfig safely retrieves the current configuration
// Multiple goroutines can safely call this method concurrently
func (cm *configManager) GetConfig() *Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Return a copy to prevent external modifications
	// This is a shallow copy, which is sufficient for our use case
	// as the Config struct fields are not modified in place
	configCopy := *cm.config
	return &configCopy
}

// ReloadConfig reads the configuration file and applies it if valid.
// The file is treated as read-only - we never modify it.
// If the new configuration is invalid, the previous configuration remains active.
func (cm *configManager) ReloadConfig() error {
	// Read the configuration file (read-only operation)
	newConfig, err := cm.loader.LoadConfig(cm.configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// Validate before applying
	if err := cm.validator.Validate(newConfig); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Atomically update the in-memory configuration
	cm.mu.Lock()
	cm.config = newConfig
	cm.mu.Unlock()

	logger.Infof("Configuration reloaded from %s", cm.configPath)
	return nil
}

// WatchConfig observes the configuration file for external changes.
// Since we never write to the file, all changes come from external sources:
// - Kubernetes ConfigMap updates (atomic via symlink swaps)
// - Docker volume mount updates
// - Configuration management tools
//
// This method blocks until the context is cancelled.
func (cm *configManager) WatchConfig(ctx context.Context) error {
	cm.watcherMu.Lock()
	if cm.watcher != nil {
		cm.watcherMu.Unlock()
		return fmt.Errorf("config watcher is already running")
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		cm.watcherMu.Unlock()
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	cm.watcher = watcher
	cm.watcherMu.Unlock()

	// Add config file to watcher
	if err := watcher.Add(cm.configPath); err != nil {
		return fmt.Errorf("failed to watch config file %s: %w", cm.configPath, err)
	}

	logger.Infof("Started watching configuration file: %s", cm.configPath)

	// Watch loop
	for {
		select {
		case <-ctx.Done():
			logger.Info("Stopping config file watcher due to context cancellation")
			return ctx.Err()

		case event, ok := <-watcher.Events:
			if !ok {
				return fmt.Errorf("watcher event channel closed")
			}

			// Detect external file modifications
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				logger.Infof("External config update detected, reloading")

				if err := cm.ReloadConfig(); err != nil {
					logger.Errorf("Failed to reload config: %v", err)
					// Continue observing - previous config remains active
				}
			}

			// Handle K8s ConfigMap updates (may remove/recreate symlinks)
			if event.Has(fsnotify.Remove) {
				logger.Debugf("Config file removed (K8s update), re-watching")
				_ = watcher.Add(cm.configPath)
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return fmt.Errorf("watcher error channel closed")
			}
			// Log error but continue watching
			logger.Errorf("File watcher error: %v", err)
		}
	}
}

// Close releases resources held by the config manager
// Specifically, it closes the file watcher if active
func (cm *configManager) Close() error {
	cm.watcherMu.Lock()
	defer cm.watcherMu.Unlock()

	if cm.watcher != nil {
		if err := cm.watcher.Close(); err != nil {
			return fmt.Errorf("failed to close file watcher: %w", err)
		}
		cm.watcher = nil
		logger.Info("Config watcher closed")
	}

	return nil
}
