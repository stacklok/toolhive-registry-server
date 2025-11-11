package helpers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/onsi/gomega"
)

// GitTestHelper manages Git repositories for testing
type GitTestHelper struct {
	ctx          context.Context
	tempDir      string
	repositories []*GitTestRepository
}

// GitTestRepository represents a test Git repository
type GitTestRepository struct {
	Name     string
	Path     string
	CloneURL string
}

// NewGitTestHelper creates a new Git test helper
func NewGitTestHelper(ctx context.Context) *GitTestHelper {
	tempDir, err := os.MkdirTemp("", "git-test-repos-*")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	return &GitTestHelper{
		ctx:          ctx,
		tempDir:      tempDir,
		repositories: make([]*GitTestRepository, 0),
	}
}

// CreateRepository creates a new Git repository for testing
func (g *GitTestHelper) CreateRepository(name string) *GitTestRepository {
	repoPath := filepath.Join(g.tempDir, name)
	err := os.MkdirAll(repoPath, 0750)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// Initialize Git repository with main branch
	g.runGitCommand(repoPath, "init", "--initial-branch=main")
	g.runGitCommand(repoPath, "config", "user.name", "Test User")
	g.runGitCommand(repoPath, "config", "user.email", "test@example.com")

	// Create initial commit to establish main branch
	initialFile := filepath.Join(repoPath, "README.md")
	err = os.WriteFile(initialFile, []byte("# Test Repository\n"), 0600)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	g.runGitCommand(repoPath, "add", "README.md")
	g.runGitCommand(repoPath, "commit", "-m", "Initial commit")

	repo := &GitTestRepository{
		Name:     name,
		Path:     repoPath,
		CloneURL: fmt.Sprintf("file://%s", repoPath), // Use file:// URL for local testing
	}

	g.repositories = append(g.repositories, repo)
	return repo
}

// CommitRegistryData commits registry data to the specified file in the repository
func (g *GitTestHelper) CommitRegistryData(
	repo *GitTestRepository, filename string, servers []RegistryServer, commitMessage string) {
	registryData := ToolHiveRegistryData{
		Version:     "1.0.0",
		LastUpdated: time.Now().Format(time.RFC3339),
		Servers:     ServersToMap(servers),
	}

	jsonData, err := json.MarshalIndent(registryData, "", "  ")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	filePath := filepath.Join(repo.Path, filename)

	// Create parent directories if needed
	dir := filepath.Dir(filePath)
	if dir != repo.Path {
		err = os.MkdirAll(dir, 0750)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}

	err = os.WriteFile(filePath, jsonData, 0600)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	g.runGitCommand(repo.Path, "add", filename)
	g.runGitCommand(repo.Path, "commit", "-m", commitMessage)
}

// UpdateRegistryData updates existing registry data in the repository
func (g *GitTestHelper) UpdateRegistryData(
	repo *GitTestRepository, filename string, servers []RegistryServer, commitMessage string) {
	// Just commit new data - same as CommitRegistryData
	g.CommitRegistryData(repo, filename, servers, commitMessage)
}

// CreateBranch creates a new branch and switches to it
func (g *GitTestHelper) CreateBranch(repo *GitTestRepository, branchName string) {
	g.runGitCommand(repo.Path, "checkout", "-b", branchName)
}

// SwitchBranch switches to an existing branch
func (g *GitTestHelper) SwitchBranch(repo *GitTestRepository, branchName string) {
	g.runGitCommand(repo.Path, "checkout", branchName)
}

// CreateTag creates a Git tag
func (g *GitTestHelper) CreateTag(repo *GitTestRepository, tagName, message string) {
	g.runGitCommand(repo.Path, "tag", "-a", tagName, "-m", message)
}

// CleanupRepositories removes all test repositories
func (g *GitTestHelper) CleanupRepositories() error {
	return os.RemoveAll(g.tempDir)
}

// runGitCommand runs a Git command in the specified directory
func (*GitTestHelper) runGitCommand(dir string, args ...string) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		gomega.Expect(err).NotTo(gomega.HaveOccurred(),
			"Git command failed: %s\nOutput: %s", cmd.String(), string(output))
	}
}
