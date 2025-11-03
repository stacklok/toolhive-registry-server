package git

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// TestRepoConfig contains configuration for creating a test repository
type TestRepoConfig struct {
	Files  map[string]string // Map of filename to content
	Author *object.Signature // Author for commits (uses default if nil)
}

// CreateTestRepo creates a temporary Git repository with the specified files and commits
// Returns the repository path and a cleanup function
func CreateTestRepo(t *testing.T, config TestRepoConfig) (string, func()) {
	t.Helper()

	// Create a temporary directory for the repository
	repoDir, err := os.MkdirTemp("", "git-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(repoDir)
	}

	// Initialize the repository
	repo, err := git.PlainInit(repoDir, false)
	if err != nil {
		cleanup()
		t.Fatalf("Failed to init repository: %v", err)
	}

	workTree, err := repo.Worktree()
	if err != nil {
		cleanup()
		t.Fatalf("Failed to get worktree: %v", err)
	}

	// Use default author if not provided
	author := config.Author
	if author == nil {
		author = &object.Signature{
			Name:  "Test Author",
			Email: "test@example.com",
		}
	}

	// Create and commit files
	for filename, content := range config.Files {
		filePath := filepath.Join(repoDir, filename)

		// Create parent directory if needed
		dir := filepath.Dir(filePath)
		if dir != repoDir {
			if err := os.MkdirAll(dir, 0755); err != nil {
				cleanup()
				t.Fatalf("Failed to create directory %s: %v", dir, err)
			}
		}

		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			cleanup()
			t.Fatalf("Failed to write file %s: %v", filename, err)
		}

		if _, err := workTree.Add(filename); err != nil {
			cleanup()
			t.Fatalf("Failed to add file %s: %v", filename, err)
		}
	}

	// Create the commit
	_, err = workTree.Commit("Initial commit", &git.CommitOptions{
		Author: author,
	})
	if err != nil {
		cleanup()
		t.Fatalf("Failed to commit: %v", err)
	}

	return repoDir, cleanup
}

// CreateTestRepoWithCommits creates a test repository with multiple commits
// Returns the repository path, commit hashes, and cleanup function
func CreateTestRepoWithCommits(t *testing.T, commits []TestRepoConfig) (string, []plumbing.Hash, func()) {
	t.Helper()

	// Create a temporary directory for the repository
	repoDir, err := os.MkdirTemp("", "git-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(repoDir)
	}

	// Initialize the repository
	repo, err := git.PlainInit(repoDir, false)
	if err != nil {
		cleanup()
		t.Fatalf("Failed to init repository: %v", err)
	}

	workTree, err := repo.Worktree()
	if err != nil {
		cleanup()
		t.Fatalf("Failed to get worktree: %v", err)
	}

	var commitHashes []plumbing.Hash

	// Create each commit
	for i, commitConfig := range commits {
		// Use default author if not provided
		author := commitConfig.Author
		if author == nil {
			author = &object.Signature{
				Name:  "Test Author",
				Email: "test@example.com",
			}
		}

		// Create and add files for this commit
		for filename, content := range commitConfig.Files {
			filePath := filepath.Join(repoDir, filename)

			// Create parent directory if needed
			dir := filepath.Dir(filePath)
			if dir != repoDir {
				if err := os.MkdirAll(dir, 0755); err != nil {
					cleanup()
					t.Fatalf("Failed to create directory %s: %v", dir, err)
				}
			}

			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				cleanup()
				t.Fatalf("Failed to write file %s: %v", filename, err)
			}

			if _, err := workTree.Add(filename); err != nil {
				cleanup()
				t.Fatalf("Failed to add file %s: %v", filename, err)
			}
		}

		// Create the commit
		commitHash, err := workTree.Commit("Commit "+string(rune('A'+i)), &git.CommitOptions{
			Author: author,
		})
		if err != nil {
			cleanup()
			t.Fatalf("Failed to commit: %v", err)
		}

		commitHashes = append(commitHashes, commitHash)
	}

	return repoDir, commitHashes, cleanup
}

// CreateTestRepoWithBranches creates a test repository with multiple branches
// The first commit creates the main branch, subsequent commits create additional branches
// Returns the repository path, branch names, and cleanup function
func CreateTestRepoWithBranches(t *testing.T, mainCommit TestRepoConfig, branches map[string]TestRepoConfig) (string, []string, func()) {
	t.Helper()

	// Create a temporary directory for the repository
	repoDir, err := os.MkdirTemp("", "git-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(repoDir)
	}

	// Initialize the repository
	repo, err := git.PlainInit(repoDir, false)
	if err != nil {
		cleanup()
		t.Fatalf("Failed to init repository: %v", err)
	}

	workTree, err := repo.Worktree()
	if err != nil {
		cleanup()
		t.Fatalf("Failed to get worktree: %v", err)
	}

	// Create main branch commit
	author := mainCommit.Author
	if author == nil {
		author = &object.Signature{
			Name:  "Test Author",
			Email: "test@example.com",
		}
	}

	for filename, content := range mainCommit.Files {
		filePath := filepath.Join(repoDir, filename)
		dir := filepath.Dir(filePath)
		if dir != repoDir {
			if err := os.MkdirAll(dir, 0755); err != nil {
				cleanup()
				t.Fatalf("Failed to create directory %s: %v", dir, err)
			}
		}

		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			cleanup()
			t.Fatalf("Failed to write file %s: %v", filename, err)
		}

		if _, err := workTree.Add(filename); err != nil {
			cleanup()
			t.Fatalf("Failed to add file %s: %v", filename, err)
		}
	}

	_, err = workTree.Commit("Initial commit", &git.CommitOptions{
		Author: author,
	})
	if err != nil {
		cleanup()
		t.Fatalf("Failed to commit: %v", err)
	}

	branchNames := []string{}

	// Create additional branches
	for branchName, branchConfig := range branches {
		branchNames = append(branchNames, branchName)

		// Create and checkout the branch
		branchRef := plumbing.NewBranchReferenceName(branchName)
		err = workTree.Checkout(&git.CheckoutOptions{
			Branch: branchRef,
			Create: true,
		})
		if err != nil {
			cleanup()
			t.Fatalf("Failed to create and checkout branch %s: %v", branchName, err)
		}

		// Add files for this branch
		branchAuthor := branchConfig.Author
		if branchAuthor == nil {
			branchAuthor = author
		}

		for filename, content := range branchConfig.Files {
			filePath := filepath.Join(repoDir, filename)
			dir := filepath.Dir(filePath)
			if dir != repoDir {
				if err := os.MkdirAll(dir, 0755); err != nil {
					cleanup()
					t.Fatalf("Failed to create directory %s: %v", dir, err)
				}
			}

			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				cleanup()
				t.Fatalf("Failed to write file %s: %v", filename, err)
			}

			if _, err := workTree.Add(filename); err != nil {
				cleanup()
				t.Fatalf("Failed to add file %s: %v", filename, err)
			}
		}

		_, err = workTree.Commit("Add "+branchName, &git.CommitOptions{
			Author: branchAuthor,
		})
		if err != nil {
			cleanup()
			t.Fatalf("Failed to commit branch %s: %v", branchName, err)
		}
	}

	return repoDir, branchNames, cleanup
}
