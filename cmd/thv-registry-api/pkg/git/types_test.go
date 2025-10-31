package git

import (
	"testing"

	"github.com/go-git/go-git/v5"
)

const (
	testRepoURL = "https://github.com/example/repo.git"
	mainBranch  = "main"
)

func TestCloneConfig_BasicValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		config      CloneConfig
		expectValid bool
	}{
		{
			name: "valid config with URL",
			config: CloneConfig{
				URL: testRepoURL,
			},
			expectValid: true,
		},
		{
			name: "valid config with branch",
			config: CloneConfig{
				URL:    testRepoURL,
				Branch: mainBranch,
			},
			expectValid: true,
		},
		{
			name: "valid config with tag",
			config: CloneConfig{
				URL: testRepoURL,
				Tag: "v1.0.0",
			},
			expectValid: true,
		},
		{
			name: "valid config with commit",
			config: CloneConfig{
				URL:    testRepoURL,
				Commit: "abc123def456",
			},
			expectValid: true,
		},
		{
			name: "invalid config - empty URL",
			config: CloneConfig{
				URL: "",
			},
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Basic validation - check that required fields are not empty
			hasURL := tt.config.URL != ""
			isValid := hasURL

			if tt.expectValid && !isValid {
				t.Errorf("Expected config to be valid, but URL=%q", tt.config.URL)
			}
			if !tt.expectValid && isValid {
				t.Errorf("Expected config to be invalid, but URL=%q", tt.config.URL)
			}
		})
	}
}

func TestCloneConfig_Fields(t *testing.T) {
	t.Parallel()
	config := CloneConfig{
		URL:    testRepoURL,
		Branch: "feature-branch",
		Tag:    "v2.0.0",
		Commit: "def456abc789",
	}

	if config.URL != testRepoURL {
		t.Errorf("Expected URL to be %q, got %q", testRepoURL, config.URL)
	}
	if config.Branch != "feature-branch" {
		t.Errorf("Expected Branch to be 'feature-branch', got %q", config.Branch)
	}
	if config.Tag != "v2.0.0" {
		t.Errorf("Expected Tag to be 'v2.0.0', got %q", config.Tag)
	}
	if config.Commit != "def456abc789" {
		t.Errorf("Expected Commit to be 'def456abc789', got %q", config.Commit)
	}
}

func TestRepositoryInfo_Fields(t *testing.T) {
	t.Parallel()
	repo := &git.Repository{} // Mock repository
	repoInfo := RepositoryInfo{
		Repository: repo,
		Branch:     mainBranch,
		RemoteURL:  testRepoURL,
	}

	if repoInfo.Repository != repo {
		t.Error("Expected Repository to be set correctly")
	}
	if repoInfo.Branch != mainBranch {
		t.Errorf("Expected Branch to be %q, got %q", mainBranch, repoInfo.Branch)
	}
	if repoInfo.RemoteURL != testRepoURL {
		t.Errorf("Expected RemoteURL to be %q, got %q", testRepoURL, repoInfo.RemoteURL)
	}
}

func TestRepositoryInfo_EmptyValues(t *testing.T) {
	t.Parallel()
	repoInfo := RepositoryInfo{}

	if repoInfo.Repository != nil {
		t.Error("Expected Repository to be nil")
	}
	if repoInfo.Branch != "" {
		t.Errorf("Expected Branch to be empty, got %q", repoInfo.Branch)
	}
	if repoInfo.RemoteURL != "" {
		t.Errorf("Expected RemoteURL to be empty, got %q", repoInfo.RemoteURL)
	}
}

func TestCloneConfig_EmptyOptionalFields(t *testing.T) {
	t.Parallel()
	config := CloneConfig{
		URL: testRepoURL,
		// Branch, Tag, Commit are intentionally empty
	}

	if config.URL == "" {
		t.Error("Expected URL to be set")
	}
	if config.Branch != "" {
		t.Errorf("Expected Branch to be empty, got %q", config.Branch)
	}
	if config.Tag != "" {
		t.Errorf("Expected Tag to be empty, got %q", config.Tag)
	}
	if config.Commit != "" {
		t.Errorf("Expected Commit to be empty, got %q", config.Commit)
	}
}

func TestCloneConfig_MutuallyExclusiveFields(t *testing.T) {
	t.Parallel()
	// This test documents the expectation that only one of Branch, Tag, or Commit should be specified
	// The actual validation logic would be in the client implementation

	configs := []struct {
		name   string
		config CloneConfig
	}{
		{
			name: "branch only",
			config: CloneConfig{
				URL:    testRepoURL,
				Branch: mainBranch,
			},
		},
		{
			name: "tag only",
			config: CloneConfig{
				URL: testRepoURL,
				Tag: "v1.0.0",
			},
		},
		{
			name: "commit only",
			config: CloneConfig{
				URL:    testRepoURL,
				Commit: "abc123",
			},
		},
	}

	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			config := tc.config
			count := 0
			if config.Branch != "" {
				count++
			}
			if config.Tag != "" {
				count++
			}
			if config.Commit != "" {
				count++
			}

			// Should have at most one reference type specified
			if count > 1 {
				t.Errorf("Only one of Branch, Tag, or Commit should be specified, but found %d", count)
			}
		})
	}
}

func TestCloneConfig_AllFieldsSet(t *testing.T) {
	t.Parallel()
	// Test that we can set all fields
	config := CloneConfig{
		URL:    testRepoURL,
		Branch: "develop",
		Tag:    "v1.2.3",
		Commit: "abcdef123456",
	}

	// Verify all fields are accessible
	if config.URL == "" {
		t.Error("URL should be set")
	}
	if config.Branch == "" {
		t.Error("Branch should be set")
	}
	if config.Tag == "" {
		t.Error("Tag should be set")
	}
	if config.Commit == "" {
		t.Error("Commit should be set")
	}
}

func TestRepositoryInfo_AllFieldsSet(t *testing.T) {
	t.Parallel()
	// Test that we can set all fields
	repo := &git.Repository{}
	repoInfo := RepositoryInfo{
		Repository: repo,
		Branch:     "feature",
		RemoteURL:  "https://example.com/repo.git",
	}

	// Verify all fields are accessible
	if repoInfo.Repository == nil {
		t.Error("Repository should be set")
	}
	if repoInfo.Branch == "" {
		t.Error("Branch should be set")
	}
	if repoInfo.RemoteURL == "" {
		t.Error("RemoteURL should be set")
	}
}
