package git

import (
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	mainBranchName = "main"
)

// TestDefaultGitClient_FullWorkflow tests a complete Git workflow with a real repository
func TestDefaultGitClient_FullWorkflow(t *testing.T) {
	t.Parallel()

	testContent := `{"name": "test-registry", "version": "1.0.0"}`
	sourceRepoDir, cleanup := CreateTestRepo(t, TestRepoConfig{
		Files: map[string]string{
			"registry.json": testContent,
		},
	})
	defer cleanup()

	// Test the full workflow
	client := NewDefaultGitClient()
	ctx := log.IntoContext(t.Context(), logr.Discard())

	// Clone the repository
	config := &CloneConfig{
		URL: sourceRepoDir, // Use local path for testing
	}

	repoInfo, err := client.Clone(ctx, config)
	if err != nil {
		t.Fatalf("Failed to clone repository: %v", err)
	}

	// Verify repository info was populated
	if repoInfo.Repository == nil {
		t.Error("Repository should not be nil")
	}
	if repoInfo.RemoteURL != sourceRepoDir {
		t.Errorf("Expected RemoteURL to be %s, got %s", sourceRepoDir, repoInfo.RemoteURL)
	}

	// Test GetFileContent
	content, err := client.GetFileContent(repoInfo, "registry.json")
	if err != nil {
		t.Fatalf("Failed to get file content: %v", err)
	}
	if string(content) != testContent {
		t.Errorf("Expected content %q, got %q", testContent, string(content))
	}

	// Test GetFileContent with non-existent file
	_, err = client.GetFileContent(repoInfo, "nonexistent.json")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}

	// Test Cleanup
	err = client.Cleanup(ctx, repoInfo)
	if err != nil {
		t.Fatalf("Failed to cleanup: %v", err)
	}
}

// TestDefaultGitClient_CloneWithBranch tests cloning with a specific branch
func TestDefaultGitClient_CloneWithBranch(t *testing.T) {
	t.Parallel()

	// Create a test repository with main and feature branches
	sourceRepoDir, _, cleanup := CreateTestRepoWithBranches(t,
		TestRepoConfig{
			Files: map[string]string{
				"mainBranchName.txt": "mainBranchName branch content",
			},
		},
		map[string]TestRepoConfig{
			"feature": {
				Files: map[string]string{
					"feature.txt": "feature branch content",
				},
			},
		},
	)
	defer cleanup()

	// Clone the feature branch
	client := NewDefaultGitClient()
	ctx := log.IntoContext(t.Context(), logr.Discard())
	config := &CloneConfig{
		URL:    sourceRepoDir,
		Branch: "feature",
	}

	repoInfo, err := client.Clone(ctx, config)
	if err != nil {
		t.Fatalf("Failed to clone feature branch: %v", err)
	}

	// Verify we're on the feature branch and have the feature file
	content, err := client.GetFileContent(repoInfo, "feature.txt")
	if err != nil {
		t.Fatalf("Failed to get feature file content: %v", err)
	}
	if string(content) != "feature branch content" {
		t.Errorf("Expected feature content, got %q", string(content))
	}

	// Verify we also have mainBranchName.txt (since feature branch was created from mainBranchName)
	mainBranchNameContent, err := client.GetFileContent(repoInfo, "mainBranchName.txt")
	if err != nil {
		t.Fatalf("Failed to get mainBranchName.txt content: %v", err)
	}
	if string(mainBranchNameContent) != "mainBranchName branch content" {
		t.Errorf("Expected mainBranchName branch content, got %q", string(mainBranchNameContent))
	}

	// Clean up
	err = client.Cleanup(ctx, repoInfo)
	if err != nil {
		t.Fatalf("Failed to cleanup: %v", err)
	}
}

// TestDefaultGitClient_CloneWithCommit tests cloning with a specific commit
func TestDefaultGitClient_CloneWithCommit(t *testing.T) {
	t.Parallel()

	// Create a test repository with two commits
	commits := []TestRepoConfig{
		{
			Files: map[string]string{
				"file1.txt": "first commit",
			},
		},
		{
			Files: map[string]string{
				"file2.txt": "second commit",
			},
		},
	}

	sourceRepoDir, commitHashes, cleanup := CreateTestRepoWithCommits(t, commits)
	defer cleanup()

	firstCommit := commitHashes[0]

	// Clone at the first commit
	client := NewDefaultGitClient()
	ctx := log.IntoContext(t.Context(), logr.Discard())
	config := &CloneConfig{
		URL:    sourceRepoDir,
		Commit: firstCommit.String(),
	}

	repoInfo, err := client.Clone(ctx, config)
	if err != nil {
		t.Fatalf("Failed to clone at specific commit: %v", err)
	}

	// Verify we have the first file
	content, err := client.GetFileContent(repoInfo, "file1.txt")
	if err != nil {
		t.Fatalf("Failed to get first file content: %v", err)
	}
	if string(content) != "first commit" {
		t.Errorf("Expected first commit content, got %q", string(content))
	}

	// Verify we don't have the second file (since we're at first commit)
	_, err = client.GetFileContent(repoInfo, "file2.txt")
	if err == nil {
		t.Error("Expected error for file2.txt not present at first commit")
	}

	// Clean up
	err = client.Cleanup(ctx, repoInfo)
	if err != nil {
		t.Fatalf("Failed to cleanup: %v", err)
	}
}

// TestDefaultGitClient_UpdateRepositoryInfo tests the updateRepositoryInfo method
func TestDefaultGitClient_UpdateRepositoryInfo(t *testing.T) {
	t.Parallel()

	tempRepoDir, cleanup := CreateTestRepo(t, TestRepoConfig{
		Files: map[string]string{
			"test.txt": "test content",
		},
	})
	defer cleanup()

	// Re-open the repository to get access to its internals
	repo, err := git.PlainOpen(tempRepoDir)
	if err != nil {
		t.Fatalf("Failed to open repository: %v", err)
	}

	workTree, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Failed to get worktree: %v", err)
	}

	// Get the current commit hash
	ref, err := repo.Head()
	if err != nil {
		t.Fatalf("Failed to get HEAD: %v", err)
	}
	commitHash := ref.Hash()

	// Create a branch
	branchRef := plumbing.NewBranchReferenceName(mainBranchName)
	err = repo.Storer.SetReference(plumbing.NewHashReference(branchRef, commitHash))
	if err != nil {
		t.Fatalf("Failed to set branch reference: %v", err)
	}

	// Checkout the branch to set HEAD properly
	err = workTree.Checkout(&git.CheckoutOptions{
		Branch: branchRef,
	})
	if err != nil {
		t.Fatalf("Failed to checkout branch: %v", err)
	}

	client := NewDefaultGitClient()
	repoInfo := &RepositoryInfo{
		Repository: repo,
	}

	// Test updateRepositoryInfo
	err = client.updateRepositoryInfo(repoInfo)
	if err != nil {
		t.Fatalf("updateRepositoryInfo failed: %v", err)
	}

	// Verify the repository info was updated correctly
	if repoInfo.Branch != mainBranchName {
		t.Errorf("Expected Branch to be %q, got %s", mainBranchName, repoInfo.Branch)
	}
}
