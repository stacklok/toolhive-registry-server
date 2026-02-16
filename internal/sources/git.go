package sources

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"runtime"
	"time"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	git2 "github.com/stacklok/toolhive-registry-server/internal/git"
)

const (
	// DefaultRegistryDataFile is the default file name for the registry data in Git sources
	DefaultRegistryDataFile = "registry.json"
)

// gitRegistryHandler handles registry data from Git repositories
type gitRegistryHandler struct {
	gitClient git2.Client
	validator RegistryDataValidator
}

// NewGitRegistryHandler creates a new Git registry handler
func NewGitRegistryHandler() RegistryHandler {
	return &gitRegistryHandler{
		gitClient: git2.NewDefaultGitClient(),
		validator: NewRegistryDataValidator(),
	}
}

// Validate validates the Git registry configuration
func (*gitRegistryHandler) Validate(regCfg *config.RegistryConfig) error {
	if regCfg == nil {
		return fmt.Errorf("registry configuration cannot be nil")
	}

	if regCfg.Git == nil {
		return fmt.Errorf("git configuration is required")
	}

	gitSource := regCfg.Git

	if gitSource.Repository == "" {
		return fmt.Errorf("git repository URL cannot be empty")
	}

	// Validate mutually exclusive branch/tag/commit
	specified := 0
	if gitSource.Branch != "" {
		specified++
	}
	if gitSource.Tag != "" {
		specified++
	}
	if gitSource.Commit != "" {
		specified++
	}

	if specified > 1 {
		return fmt.Errorf("only one of branch, tag, or commit may be specified")
	}

	// Set default path if not specified
	if gitSource.Path == "" {
		gitSource.Path = DefaultRegistryDataFile
	}

	return nil
}

// fetchRegistryData retrieves registry data from the Git repository
func (h *gitRegistryHandler) fetchRegistryData(ctx context.Context, regCfg *config.RegistryConfig) ([]byte, error) {

	// Validate registry configuration
	if err := h.Validate(regCfg); err != nil {
		return nil, fmt.Errorf("registry validation failed: %w", err)
	}

	gitSource := regCfg.Git
	// Prepare clone configuration
	cloneConfig := &git2.CloneConfig{
		URL:    gitSource.Repository,
		Branch: gitSource.Branch,
		Tag:    gitSource.Tag,
		Commit: gitSource.Commit,
	}

	// Configure authentication if provided
	if gitSource.Auth != nil && gitSource.Auth.Username != "" {
		password, err := gitSource.Auth.GetPassword()
		if err != nil {
			return nil, fmt.Errorf("failed to get git password: %w", err)
		}
		cloneConfig.Auth = &git2.AuthConfig{
			Username: gitSource.Auth.Username,
			Password: password,
		}
	}

	// Clone the repository with timing and metrics
	startTime := time.Now()
	slog.Info("Starting git clone",
		"repository", cloneConfig.URL,
		"branch", cloneConfig.Branch,
		"tag", cloneConfig.Tag,
		"commit", cloneConfig.Commit)

	// Capture memory stats before operation
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)
	repoInfo, err := h.gitClient.Clone(ctx, cloneConfig)
	cloneDuration := time.Since(startTime)

	if err != nil {
		slog.Error("Git clone failed",
			"error", err,
			"repository", cloneConfig.URL,
			"duration", cloneDuration.String())
		return nil, fmt.Errorf("failed to clone repository: %w", err)
	}

	cloneAttrs := []any{
		"repository", cloneConfig.URL,
		"duration", cloneDuration.String(),
		"branch", repoInfo.Branch,
	}
	if repoInfo.Repository != nil {
		if ref, err := repoInfo.Repository.Head(); err == nil {
			cloneAttrs = append(cloneAttrs, "commit_sha", ref.Hash().String())
		}
	}
	slog.Info("Git clone completed", cloneAttrs...)

	// Ensure cleanup
	defer func() {
		if cleanupErr := h.gitClient.Cleanup(ctx, repoInfo); cleanupErr != nil {
			// Log error but don't fail the operation
			slog.Error("Failed to cleanup repository", "error", cleanupErr)
		}
		logMemoryStatsAfterOperation(&memBefore)
	}()

	// Get file content from repository
	filePath := gitSource.Path
	if filePath == "" {
		filePath = DefaultRegistryDataFile
	}

	registryData, err := h.gitClient.GetFileContent(repoInfo, filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get file %s from repository: %w", filePath, err)
	}

	return registryData, nil
}

// FetchRegistry retrieves registry data from the Git repository
func (h *gitRegistryHandler) FetchRegistry(ctx context.Context, regCfg *config.RegistryConfig) (*FetchResult, error) {

	registryData, err := h.fetchRegistryData(ctx, regCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch registry data: %w", err)
	}

	// Validate and parse registry data
	reg, err := h.validator.ValidateData(registryData, regCfg.Format)
	if err != nil {
		return nil, fmt.Errorf("registry data validation failed: %w", err)
	}

	// Calculate hash using the SHA256 hash of the registry data
	hash := fmt.Sprintf("%x", sha256.Sum256(registryData))

	// Create and return fetch result with pre-calculated hash
	return NewFetchResult(reg, hash, regCfg.Format), nil
}

// CurrentHash returns the current hash of the source data after fetching the registry data
func (h *gitRegistryHandler) CurrentHash(ctx context.Context, regCfg *config.RegistryConfig) (string, error) {
	registryData, err := h.fetchRegistryData(ctx, regCfg)
	if err != nil {
		return "", fmt.Errorf("failed to fetch registry data: %w", err)
	}

	// Compute and return hash of the data
	hash := fmt.Sprintf("%x", sha256.Sum256(registryData))
	return hash, nil
}

// logMemoryStatsAfterOperation logs the memory stats after an operation
func logMemoryStatsAfterOperation(memBefore *runtime.MemStats) {
	// Log memory stats after cleanup and GC
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	// Calculate delta in MB with proper signed arithmetic
	allocAfterMB := memAfter.Alloc / (1024 * 1024)
	allocBeforeMB := memBefore.Alloc / (1024 * 1024)
	var deltaMB int64
	if allocAfterMB >= allocBeforeMB {
		// #nosec G115 -- Memory delta in MB will never exceed int64 max
		deltaMB = int64(allocAfterMB - allocBeforeMB)
	} else {
		// #nosec G115 -- Memory delta in MB will never exceed int64 max
		deltaMB = -int64(allocBeforeMB - allocAfterMB)
	}

	// Calculate additional memory metrics for container memory debugging
	// These help explain the difference between Go heap and container RSS
	sysMB := memAfter.Sys / (1024 * 1024)                   // Total memory from OS
	heapAllocMB := memAfter.HeapAlloc / (1024 * 1024)       // Bytes allocated on heap
	heapSysMB := memAfter.HeapSys / (1024 * 1024)           // Bytes obtained from OS for heap
	heapReleasedMB := memAfter.HeapReleased / (1024 * 1024) // Bytes returned to OS
	heapIdleMB := memAfter.HeapIdle / (1024 * 1024)         // Bytes in idle spans

	slog.Info("Memory stats after operation",
		"alloc_mb", allocAfterMB,
		"delta_mb", deltaMB,
		"sys_mb", sysMB,
		"heap_alloc_mb", heapAllocMB,
		"heap_sys_mb", heapSysMB,
		"heap_idle_mb", heapIdleMB,
		"heap_released_mb", heapReleasedMB)
}
