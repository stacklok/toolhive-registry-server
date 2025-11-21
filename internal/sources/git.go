package sources

import (
	"context"
	"crypto/sha256"
	"fmt"
	"runtime"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	git2 "github.com/stacklok/toolhive-registry-server/internal/git"
)

const (
	// DefaultRegistryDataFile is the default file name for the registry data in Git sources
	DefaultRegistryDataFile = "registry.json"
)

// gitSourceHandler handles registry data from Git repositories
type gitSourceHandler struct {
	gitClient git2.Client
	validator SourceDataValidator
}

// NewGitSourceHandler creates a new Git source handler
func NewGitSourceHandler() SourceHandler {
	return &gitSourceHandler{
		gitClient: git2.NewDefaultGitClient(),
		validator: NewSourceDataValidator(),
	}
}

// Validate validates the Git source configuration
func (*gitSourceHandler) Validate(source *config.SourceConfig) error {
	if source.Type != config.SourceTypeGit {
		return fmt.Errorf("invalid source type: expected %s, got %s",
			config.SourceTypeGit, source.Type)
	}

	if source.Git == nil {
		return fmt.Errorf("git configuration is required for source type %s",
			config.SourceTypeGit)
	}

	gitSource := source.Git

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

// FetchRegistry retrieves registry data from the Git repository
func (h *gitSourceHandler) fetchRegistryData(ctx context.Context, cfg *config.Config) ([]byte, error) {

	// Validate source configuration
	if err := h.Validate(&cfg.Source); err != nil {
		return nil, fmt.Errorf("source validation failed: %w", err)
	}

	gitSource := cfg.Source.Git
	// Prepare clone configuration
	cloneConfig := &git2.CloneConfig{
		URL:    gitSource.Repository,
		Branch: gitSource.Branch,
		Tag:    gitSource.Tag,
		Commit: gitSource.Commit,
	}

	// Clone the repository with timing and metrics
	logger := log.FromContext(ctx)
	startTime := time.Now()
	logger.Info("Starting git clone",
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
		logger.Error(err, "Git clone failed",
			"repository", cloneConfig.URL,
			"duration", cloneDuration.String())
		return nil, fmt.Errorf("failed to clone repository: %w", err)
	}

	logger.Info("Git clone completed",
		"repository", cloneConfig.URL,
		"duration", cloneDuration.String(),
		"branch", repoInfo.Branch)

	// Ensure cleanup
	defer func() {
		if cleanupErr := h.gitClient.Cleanup(ctx, repoInfo); cleanupErr != nil {
			// Log error but don't fail the operation
			log.FromContext(ctx).Error(cleanupErr, "Failed to cleanup repository")
		}
		logMemoryStatsAfterOperation(ctx, &memBefore)
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
func (h *gitSourceHandler) FetchRegistry(ctx context.Context, cfg *config.Config) (*FetchResult, error) {

	registryData, err := h.fetchRegistryData(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch registry data: %w", err)
	}

	// Validate and parse registry data
	reg, err := h.validator.ValidateData(registryData, cfg.Source.Format)
	if err != nil {
		return nil, fmt.Errorf("registry data validation failed: %w", err)
	}

	// Calculate hash using the SHA256 hash of the registry data
	hash := fmt.Sprintf("%x", sha256.Sum256(registryData))

	// Create and return fetch result with pre-calculated hash
	return NewFetchResult(reg, hash, cfg.Source.Format), nil
}

// CurrentHash returns the current hash of the source data after fetching the registry data
func (h *gitSourceHandler) CurrentHash(ctx context.Context, cfg *config.Config) (string, error) {
	registryData, err := h.fetchRegistryData(ctx, cfg)
	if err != nil {
		return "", fmt.Errorf("failed to fetch registry data: %w", err)
	}

	// Compute and return hash of the data
	hash := fmt.Sprintf("%x", sha256.Sum256(registryData))
	return hash, nil
}

// logMemoryStatsAfterOperation logs the memory stats after an operation
func logMemoryStatsAfterOperation(ctx context.Context, memBefore *runtime.MemStats) {
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

	log.FromContext(ctx).Info("Memory stats after operation",
		"alloc_mb", allocAfterMB,
		"delta_mb", deltaMB,
		"sys_mb", sysMB,
		"heap_alloc_mb", heapAllocMB,
		"heap_sys_mb", heapSysMB,
		"heap_idle_mb", heapIdleMB,
		"heap_released_mb", heapReleasedMB)
}
