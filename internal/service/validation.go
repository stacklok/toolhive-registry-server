// Package service provides the business logic for the MCP registry API
package service

import (
	"fmt"
	"time"
)

// ValidateRegistryConfig validates a RegistryCreateRequest
func ValidateRegistryConfig(config *RegistryCreateRequest) error {
	if config == nil {
		return fmt.Errorf("config is required")
	}

	// Exactly one source type must be set
	sourceCount := config.CountSourceTypes()
	if sourceCount == 0 {
		return fmt.Errorf("one of git, api, file, managed, or kubernetes must be specified")
	}
	if sourceCount > 1 {
		return fmt.Errorf("only one source type may be specified")
	}

	// For synced registries, sync policy is required
	if !config.IsNonSyncedType() {
		if config.SyncPolicy == nil || config.SyncPolicy.Interval == "" {
			return fmt.Errorf("syncPolicy.interval is required for %s registries", config.GetSourceType())
		}
		if _, err := time.ParseDuration(config.SyncPolicy.Interval); err != nil {
			return fmt.Errorf("invalid sync interval: %w", err)
		}
	}

	// Validate format if specified
	if config.Format != "" && config.Format != "toolhive" && config.Format != "upstream" {
		return fmt.Errorf("format must be 'toolhive' or 'upstream', got '%s'", config.Format)
	}

	// Source-specific validation
	return validateSourceSpecific(config)
}

// validateSourceSpecific validates source-specific configuration
func validateSourceSpecific(config *RegistryCreateRequest) error {
	switch config.GetSourceType() {
	case SourceTypeGit:
		return validateGitConfig(config.Git)
	case SourceTypeAPI:
		return validateAPIConfig(config.API)
	case SourceTypeFile:
		return validateFileConfig(config.File)
	case SourceTypeManaged:
		// Managed registries have no required fields
		return nil
	case SourceTypeKubernetes:
		// Kubernetes registries have no required fields
		return nil
	default:
		return fmt.Errorf("unknown source type")
	}
}

// validateGitConfig validates Git source configuration
func validateGitConfig(cfg *GitSourceConfig) error {
	if cfg == nil {
		return fmt.Errorf("git config is required")
	}

	if cfg.Repository == "" {
		return fmt.Errorf("git.repository is required")
	}

	// Branch, Tag, and Commit are mutually exclusive
	refCount := 0
	if cfg.Branch != "" {
		refCount++
	}
	if cfg.Tag != "" {
		refCount++
	}
	if cfg.Commit != "" {
		refCount++
	}
	if refCount > 1 {
		return fmt.Errorf("git.branch, git.tag, and git.commit are mutually exclusive")
	}

	return nil
}

// validateAPIConfig validates API source configuration
func validateAPIConfig(cfg *APISourceConfig) error {
	if cfg == nil {
		return fmt.Errorf("api config is required")
	}

	if cfg.Endpoint == "" {
		return fmt.Errorf("api.endpoint is required")
	}

	return nil
}

// validateFileConfig validates File source configuration
func validateFileConfig(cfg *FileSourceConfig) error {
	if cfg == nil {
		return fmt.Errorf("file config is required")
	}

	if cfg.Path == "" && cfg.URL == "" {
		return fmt.Errorf("file.path or file.url is required")
	}
	if cfg.Path != "" && cfg.URL != "" {
		return fmt.Errorf("file.path and file.url are mutually exclusive")
	}

	// Validate timeout if specified
	if cfg.Timeout != "" {
		if _, err := time.ParseDuration(cfg.Timeout); err != nil {
			return fmt.Errorf("invalid file.timeout: %w", err)
		}
	}

	return nil
}
