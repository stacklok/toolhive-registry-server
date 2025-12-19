// Package service provides the business logic for the MCP registry API
package service

import (
	"fmt"
	"time"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/sources"
)

// ValidateRegistryConfig validates a RegistryCreateRequest
func ValidateRegistryConfig(req *RegistryCreateRequest) error {
	if req == nil {
		return fmt.Errorf("config is required")
	}

	// Exactly one source type must be set
	sourceCount := req.CountSourceTypes()
	if sourceCount == 0 {
		return fmt.Errorf("one of git, api, file, managed, or kubernetes must be specified")
	}
	if sourceCount > 1 {
		return fmt.Errorf("only one source type may be specified")
	}

	// For synced registries, sync policy is required
	if !req.IsNonSyncedType() {
		if req.SyncPolicy == nil || req.SyncPolicy.Interval == "" {
			return fmt.Errorf("syncPolicy.interval is required for %s registries", req.GetSourceType())
		}
		if _, err := time.ParseDuration(req.SyncPolicy.Interval); err != nil {
			return fmt.Errorf("invalid sync interval: %w", err)
		}
	}

	// Validate format if specified
	if req.Format != "" && req.Format != "toolhive" && req.Format != "upstream" {
		return fmt.Errorf("format must be 'toolhive' or 'upstream', got '%s'", req.Format)
	}

	// Source-specific validation
	return validateSourceSpecific(req)
}

// validateSourceSpecific validates source-specific configuration
func validateSourceSpecific(req *RegistryCreateRequest) error {
	switch req.GetSourceType() {
	case config.SourceTypeGit:
		return validateGitConfig(req.Git)
	case config.SourceTypeAPI:
		return validateAPIConfig(req.API)
	case config.SourceTypeFile:
		return validateFileConfig(req.File)
	case config.SourceTypeManaged:
		// Managed registries have no required fields
		return nil
	case config.SourceTypeKubernetes:
		// Kubernetes registries have no required fields
		return nil
	default:
		return fmt.Errorf("unknown source type")
	}
}

// validateGitConfig validates Git source configuration
func validateGitConfig(cfg *config.GitConfig) error {
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
func validateAPIConfig(cfg *config.APIConfig) error {
	if cfg == nil {
		return fmt.Errorf("api config is required")
	}

	if cfg.Endpoint == "" {
		return fmt.Errorf("api.endpoint is required")
	}

	return nil
}

// validateFileConfig validates File source configuration
func validateFileConfig(cfg *config.FileConfig) error {
	if cfg == nil {
		return fmt.Errorf("file config is required")
	}

	// Count how many source options are set (path, url, data are mutually exclusive)
	sourceCount := 0
	if cfg.Path != "" {
		sourceCount++
	}
	if cfg.URL != "" {
		sourceCount++
	}
	if cfg.Data != "" {
		sourceCount++
	}

	if sourceCount == 0 {
		return fmt.Errorf("file.path, file.url, or file.data is required")
	}
	if sourceCount > 1 {
		return fmt.Errorf("file.path, file.url, and file.data are mutually exclusive")
	}

	// Validate timeout if specified (only applicable for URL)
	if cfg.Timeout != "" {
		if cfg.URL == "" {
			return fmt.Errorf("file.timeout is only applicable when file.url is specified")
		}
		if _, err := time.ParseDuration(cfg.Timeout); err != nil {
			return fmt.Errorf("invalid file.timeout: %w", err)
		}
	}

	// Basic validation for inline data
	if cfg.Data != "" {
		if err := ValidateInlineDataBasic(cfg.Data); err != nil {
			return fmt.Errorf("file.data: %w", err)
		}
	}

	return nil
}

// ValidateInlineDataBasic performs basic validation on inline registry data.
// It uses the existing registry data validator to parse and validate both
// toolhive and upstream formats.
func ValidateInlineDataBasic(data string) error {
	return ValidateInlineDataWithFormat(data, "")
}

// ValidateInlineDataWithFormat validates inline registry data with a specific format.
// If format is empty, it tries upstream format first, then falls back to toolhive.
func ValidateInlineDataWithFormat(data string, format string) error {
	if data == "" {
		return fmt.Errorf("data cannot be empty")
	}

	validator := sources.NewRegistryDataValidator()

	// If format is specified, use it directly
	if format != "" {
		_, err := validator.ValidateData([]byte(data), format)
		return err
	}

	// Try upstream format first (more common for API usage), then toolhive
	_, err := validator.ValidateData([]byte(data), "upstream")
	if err != nil {
		_, err = validator.ValidateData([]byte(data), "toolhive")
	}
	return err
}
