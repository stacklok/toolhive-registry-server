package config

import (
	"context"
	"fmt"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/stacklok/toolhive/pkg/logger"
)

// ConfigManager provides thread-safe configuration management with reload capabilities.
// It supports atomic configuration updates and file watching for automatic reloads.
//
// Design decisions:
// - Uses sync.RWMutex for efficient concurrent reads with occasional writes
// - Validates configuration before applying to prevent invalid states
// - Preserves old configuration on validation or load failures
// - Optimized for container environments (Kubernetes ConfigMap updates, volume mounts)
// - Provides context-aware watching for graceful shutdown
type ConfigManager interface {
	// GetConfig safely retrieves the current configuration
	GetConfig() *Config

	// ReloadConfig atomically loads and applies a new configuration from the file path
	// Returns error if loading or validation fails, preserving the old config
	ReloadConfig() error

	// WatchConfig starts watching the configuration file for changes
	// Automatically reloads when changes are detected (with debouncing)
	// Blocks until context is cancelled or an unrecoverable error occurs
	WatchConfig(ctx context.Context) error

	// Close releases any resources held by the manager
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

// ReloadConfig atomically loads and applies a new configuration
// If loading or validation fails, the old configuration is preserved
func (cm *configManager) ReloadConfig() error {
	// Load new configuration (outside the lock to allow concurrent reads)
	newConfig, err := cm.loader.LoadConfig(cm.configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Validate new configuration
	if err := cm.validator.Validate(newConfig); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	// Atomically swap the configuration
	cm.mu.Lock()
	cm.config = newConfig
	cm.mu.Unlock()

	logger.Infof("Configuration reloaded successfully from %s", cm.configPath)
	return nil
}

// WatchConfig starts watching the configuration file for changes.
// It uses fsnotify to detect file system events.
//
// In container environments, config updates typically happen atomically through:
// - Kubernetes ConfigMap volume mounts (using symlink swaps)
// - Docker volume updates
// - Orchestration tool updates
//
// Blocks until the context is cancelled or an unrecoverable error occurs.
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

			// In containers, we primarily care about Write events
			// Kubernetes ConfigMaps update files atomically via symlink swaps
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				logger.Infof("Config file change detected (%s), reloading configuration", event.Op)

				// Reload immediately - container environments have atomic updates
				if err := cm.ReloadConfig(); err != nil {
					logger.Errorf("Failed to reload config after file change: %v", err)
					// Continue watching - old configuration remains active
				}
			}

			// Handle Remove events for Kubernetes ConfigMap updates
			// K8s may remove and recreate the symlink during updates
			if event.Has(fsnotify.Remove) {
				logger.Debugf("Config file removed (likely K8s ConfigMap update), re-watching")
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
