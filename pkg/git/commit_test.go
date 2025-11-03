package git

import (
	"testing"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// TestDefaultGitClient_CloneSpecificCommit tests cloning a specific commit
func TestDefaultGitClient_CloneSpecificCommit(t *testing.T) {
	t.Parallel()

	// Create a test repository with multiple commits
	commits := []TestRepoConfig{
		{
			Files: map[string]string{
				"registry.json": `{"name": "test-registry", "version": "1.0.0"}`,
			},
		},
		{
			Files: map[string]string{
				"registry.json": `{"name": "test-registry", "version": "2.0.0"}`,
			},
		},
	}

	repoDir, commitHashes, cleanup := CreateTestRepoWithCommits(t, commits)
	defer cleanup()

	client := NewDefaultGitClient()
	ctx := log.IntoContext(t.Context(), logr.Discard())

	// Clone at the first commit
	config := &CloneConfig{
		URL:    repoDir,
		Commit: commitHashes[0].String(),
	}

	repoInfo, err := client.Clone(ctx, config)
	if err != nil {
		t.Fatalf("Failed to clone repository at specific commit: %v", err)
	}

	// Verify we can read the registry file at the first commit
	content, err := client.GetFileContent(repoInfo, "registry.json")
	if err != nil {
		t.Fatalf("Failed to get registry file content: %v", err)
	}

	expectedContent := `{"name": "test-registry", "version": "1.0.0"}`
	if string(content) != expectedContent {
		t.Errorf("Expected content from first commit %q, got %q", expectedContent, string(content))
	}

	// Clean up
	err = client.Cleanup(ctx, repoInfo)
	if err != nil {
		t.Fatalf("Failed to cleanup: %v", err)
	}
}

// TestDefaultGitClient_CloneInvalidCommit tests error handling for invalid commit hash
func TestDefaultGitClient_CloneInvalidCommit(t *testing.T) {
	t.Parallel()

	// Create a simple test repository
	repoDir, cleanup := CreateTestRepo(t, TestRepoConfig{
		Files: map[string]string{
			"test.txt": "test content",
		},
	})
	defer cleanup()

	client := NewDefaultGitClient()
	ctx := log.IntoContext(t.Context(), logr.Discard())

	// Test with a short invalid commit hash (will fail validation)
	config := &CloneConfig{
		URL:    repoDir,
		Commit: "f4da6f2", // Short hash that doesn't exist
	}

	repoInfo, err := client.Clone(ctx, config)
	if err == nil {
		t.Error("Expected error for invalid commit hash, got nil")
		if repoInfo != nil {
			_ = client.Cleanup(ctx, repoInfo)
		}
	}

	if repoInfo != nil {
		t.Error("Expected nil repoInfo for invalid commit")
	}
}
