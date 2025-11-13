package sources

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"

	"github.com/stacklok/toolhive/pkg/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/git"
)

const (
	testGitRepoURL = "https://github.com/example/test-repo.git"
	testBranch     = "main"
	testTag        = "v1.0.0"
	testCommit     = "abc123def456"
	testFilePath   = "custom-registry.json"
)

// MockGitClient is a mock implementation of git.Client
type MockGitClient struct {
	mock.Mock
}

func (m *MockGitClient) Clone(ctx context.Context, cfg *git.CloneConfig) (*git.RepositoryInfo, error) {
	args := m.Called(ctx, cfg)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*git.RepositoryInfo), args.Error(1)
}

func (m *MockGitClient) Pull(ctx context.Context, repoInfo *git.RepositoryInfo) error {
	args := m.Called(ctx, repoInfo)
	return args.Error(0)
}

func (m *MockGitClient) GetFileContent(repoInfo *git.RepositoryInfo, path string) ([]byte, error) {
	args := m.Called(repoInfo, path)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockGitClient) GetCommitHash(repoInfo *git.RepositoryInfo) (string, error) {
	args := m.Called(repoInfo)
	return args.String(0), args.Error(1)
}

func (m *MockGitClient) Cleanup(_ context.Context, repoInfo *git.RepositoryInfo) error {
	args := m.Called(repoInfo)
	return args.Error(0)
}

// MockSourceDataValidator is a mock implementation of SourceDataValidator
type MockSourceDataValidator struct {
	mock.Mock
}

func (m *MockSourceDataValidator) ValidateData(data []byte, format string) (*registry.Registry, error) {
	args := m.Called(data, format)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*registry.Registry), args.Error(1)
}

func TestNewGitSourceHandler(t *testing.T) {
	t.Parallel()

	handler := NewGitSourceHandler()

	assert.NotNil(t, handler)
	assert.NotNil(t, handler.gitClient)
	assert.NotNil(t, handler.validator)
}

func TestGitSourceHandler_Validate(t *testing.T) {
	t.Parallel()

	handler := NewGitSourceHandler()

	tests := []struct {
		name        string
		source      *config.SourceConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid git source with repository only",
			source: &config.SourceConfig{
				Type: config.SourceTypeGit,
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
				},
			},
			expectError: false,
		},
		{
			name: "valid git source with branch",
			source: &config.SourceConfig{
				Type: config.SourceTypeGit,
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
					Branch:     testBranch,
				},
			},
			expectError: false,
		},
		{
			name: "valid git source with tag",
			source: &config.SourceConfig{
				Type: config.SourceTypeGit,
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
					Tag:        testTag,
				},
			},
			expectError: false,
		},
		{
			name: "valid git source with commit",
			source: &config.SourceConfig{
				Type: config.SourceTypeGit,
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
					Commit:     testCommit,
				},
			},
			expectError: false,
		},
		{
			name: "valid git source with custom path",
			source: &config.SourceConfig{
				Type: config.SourceTypeGit,
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
					Path:       testFilePath,
				},
			},
			expectError: false,
		},
		{
			name: "invalid source type",
			source: &config.SourceConfig{
				Type: config.SourceTypeFile,
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
				},
			},
			expectError: true,
			errorMsg:    "invalid source type",
		},
		{
			name: "missing git configuration",
			source: &config.SourceConfig{
				Type: config.SourceTypeGit,
				Git:  nil,
			},
			expectError: true,
			errorMsg:    "git configuration is required",
		},
		{
			name: "empty repository URL",
			source: &config.SourceConfig{
				Type: config.SourceTypeGit,
				Git: &config.GitConfig{
					Repository: "",
				},
			},
			expectError: true,
			errorMsg:    "git repository URL cannot be empty",
		},
		{
			name: "multiple reference types - branch and tag",
			source: &config.SourceConfig{
				Type: config.SourceTypeGit,
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
					Branch:     testBranch,
					Tag:        testTag,
				},
			},
			expectError: true,
			errorMsg:    "only one of branch, tag, or commit may be specified",
		},
		{
			name: "multiple reference types - branch and commit",
			source: &config.SourceConfig{
				Type: config.SourceTypeGit,
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
					Branch:     testBranch,
					Commit:     testCommit,
				},
			},
			expectError: true,
			errorMsg:    "only one of branch, tag, or commit may be specified",
		},
		{
			name: "multiple reference types - tag and commit",
			source: &config.SourceConfig{
				Type: config.SourceTypeGit,
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
					Tag:        testTag,
					Commit:     testCommit,
				},
			},
			expectError: true,
			errorMsg:    "only one of branch, tag, or commit may be specified",
		},
		{
			name: "all reference types specified",
			source: &config.SourceConfig{
				Type: config.SourceTypeGit,
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
					Branch:     testBranch,
					Tag:        testTag,
					Commit:     testCommit,
				},
			},
			expectError: true,
			errorMsg:    "only one of branch, tag, or commit may be specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := handler.Validate(tt.source)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
				// Check that default path is set when not specified
				if tt.source.Git != nil && tt.source.Git.Path == "" {
					assert.Equal(t, DefaultRegistryDataFile, tt.source.Git.Path)
				}
			}
		})
	}
}

func TestGitSourceHandler_FetchRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		config        *config.Config
		setupMocks    func(*MockGitClient, *MockSourceDataValidator)
		expectError   bool
		errorContains string
	}{
		{
			name: "successful fetch with default path",
			config: &config.Config{
				Source: config.SourceConfig{
					Type:   config.SourceTypeGit,
					Format: config.SourceFormatToolHive,
					Git: &config.GitConfig{
						Repository: testGitRepoURL,
						Branch:     testBranch,
					},
				},
			},
			setupMocks: func(gitClient *MockGitClient, validator *MockSourceDataValidator) {
				repoInfo := &git.RepositoryInfo{
					RemoteURL: testGitRepoURL,
				}
				testData := []byte(`{"version": "1.0.0"}`)
				testRegistry := &registry.Registry{
					Version:       "1.0.0",
					Servers:       make(map[string]*registry.ImageMetadata),
					RemoteServers: make(map[string]*registry.RemoteServerMetadata),
				}

				gitClient.On("Clone", mock.Anything, mock.MatchedBy(func(config *git.CloneConfig) bool {
					return config.URL == testGitRepoURL && config.Branch == testBranch
				})).Return(repoInfo, nil)

				gitClient.On("GetFileContent", repoInfo, DefaultRegistryDataFile).Return(testData, nil)
				gitClient.On("Cleanup", repoInfo).Return(nil)

				validator.On("ValidateData", testData, config.SourceFormatToolHive).Return(testRegistry, nil)
			},
			expectError: false,
		},
		{
			name: "successful fetch with custom path",
			config: &config.Config{
				Source: config.SourceConfig{
					Type:   config.SourceTypeGit,
					Format: config.SourceFormatToolHive,
					Git: &config.GitConfig{
						Repository: testGitRepoURL,
						Tag:        testTag,
						Path:       testFilePath,
					},
				},
			},
			setupMocks: func(gitClient *MockGitClient, validator *MockSourceDataValidator) {
				repoInfo := &git.RepositoryInfo{
					RemoteURL: testGitRepoURL,
				}
				testData := []byte(`{"version": "1.0.0"}`)
				testRegistry := &registry.Registry{
					Version:       "1.0.0",
					Servers:       make(map[string]*registry.ImageMetadata),
					RemoteServers: make(map[string]*registry.RemoteServerMetadata),
				}

				gitClient.On("Clone", mock.Anything, mock.MatchedBy(func(config *git.CloneConfig) bool {
					return config.URL == testGitRepoURL && config.Tag == testTag
				})).Return(repoInfo, nil)

				gitClient.On("GetFileContent", repoInfo, testFilePath).Return(testData, nil)
				gitClient.On("Cleanup", repoInfo).Return(nil)

				validator.On("ValidateData", testData, config.SourceFormatToolHive).Return(testRegistry, nil)
			},
			expectError: false,
		},
		{
			name: "validation failure",
			config: &config.Config{
				Source: config.SourceConfig{
					Format: config.SourceFormatToolHive,
					Type:   config.SourceTypeGit,
					Git: &config.GitConfig{
						Repository: "", // Invalid
					},
				},
			},
			setupMocks: func(_ *MockGitClient, _ *MockSourceDataValidator) {
				// No mocks needed as validation should fail before git operations
			},
			expectError:   true,
			errorContains: "source validation failed",
		},
		{
			name: "clone failure",
			config: &config.Config{
				Source: config.SourceConfig{
					Format: config.SourceFormatToolHive,
					Type:   config.SourceTypeGit,
					Git: &config.GitConfig{
						Repository: testGitRepoURL,
						Commit:     testCommit,
					},
				},
			},
			setupMocks: func(gitClient *MockGitClient, _ *MockSourceDataValidator) {
				gitClient.On("Clone", mock.Anything, mock.MatchedBy(func(config *git.CloneConfig) bool {
					return config.URL == testGitRepoURL && config.Commit == testCommit
				})).Return(nil, errors.New("clone failed"))
			},
			expectError:   true,
			errorContains: "failed to clone repository",
		},
		{
			name: "file not found",
			config: &config.Config{
				Source: config.SourceConfig{
					Format: config.SourceFormatToolHive,
					Type:   config.SourceTypeGit,
					Git: &config.GitConfig{
						Repository: testGitRepoURL,
					},
				},
			},
			setupMocks: func(gitClient *MockGitClient, _ *MockSourceDataValidator) {
				repoInfo := &git.RepositoryInfo{
					RemoteURL: testGitRepoURL,
				}

				gitClient.On("Clone", mock.Anything, mock.Anything).Return(repoInfo, nil)
				gitClient.On("GetFileContent", repoInfo, DefaultRegistryDataFile).Return(nil, errors.New("file not found"))
				gitClient.On("Cleanup", repoInfo).Return(nil)
			},
			expectError:   true,
			errorContains: "failed to get file",
		},
		{
			name: "validation data failure",
			config: &config.Config{
				Source: config.SourceConfig{
					Format: config.SourceFormatToolHive,
					Type:   config.SourceTypeGit,
					Git: &config.GitConfig{
						Repository: testGitRepoURL,
					},
				},
			},
			setupMocks: func(gitClient *MockGitClient, validator *MockSourceDataValidator) {
				repoInfo := &git.RepositoryInfo{
					RemoteURL: testGitRepoURL,
				}
				testData := []byte(`invalid json`)

				gitClient.On("Clone", mock.Anything, mock.Anything).Return(repoInfo, nil)
				gitClient.On("GetFileContent", repoInfo, DefaultRegistryDataFile).Return(testData, nil)
				gitClient.On("Cleanup", repoInfo).Return(nil)

				validator.On("ValidateData", testData, config.SourceFormatToolHive).Return(nil, errors.New("invalid data"))
			},
			expectError:   true,
			errorContains: "registry data validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create mocks
			mockGitClient := new(MockGitClient)
			mockValidator := new(MockSourceDataValidator)

			// Setup mocks
			tt.setupMocks(mockGitClient, mockValidator)

			// Create handler with mocks
			handler := &GitSourceHandler{
				gitClient: mockGitClient,
				validator: mockValidator,
			}

			// Execute test
			result, err := handler.FetchRegistry(context.Background(), tt.config)

			// Verify results
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, result)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.NotEmpty(t, result.Hash)
				assert.Equal(t, tt.config.Source.Format, result.Format)
			}

			// Verify all mock expectations
			mockGitClient.AssertExpectations(t)
			mockValidator.AssertExpectations(t)
		})
	}
}

func TestGitSourceHandler_CurrentHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		config        *config.Config
		setupMocks    func(*MockGitClient)
		expectError   bool
		errorContains string
		expectedHash  string
	}{
		{
			name: "successful hash calculation",
			config: &config.Config{
				Source: config.SourceConfig{
					Format: config.SourceFormatToolHive,
					Type:   config.SourceTypeGit,
					Git: &config.GitConfig{
						Repository: testGitRepoURL,
						Branch:     testBranch,
					},
				},
			},
			setupMocks: func(gitClient *MockGitClient) {
				repoInfo := &git.RepositoryInfo{
					RemoteURL: testGitRepoURL,
				}
				testData := []byte(`{"version": "1.0.0"}`)

				gitClient.On("Clone", mock.Anything, mock.MatchedBy(func(config *git.CloneConfig) bool {
					return config.URL == testGitRepoURL && config.Branch == testBranch
				})).Return(repoInfo, nil)

				gitClient.On("GetFileContent", repoInfo, DefaultRegistryDataFile).Return(testData, nil)
				gitClient.On("Cleanup", repoInfo).Return(nil)
			},
			expectError:  false,
			expectedHash: fmt.Sprintf("%x", sha256.Sum256([]byte(`{"version": "1.0.0"}`))),
		},
		{
			name: "validation failure",
			config: &config.Config{
				Source: config.SourceConfig{
					Format: config.SourceFormatToolHive,
					Type:   config.SourceTypeGit,
					Git: &config.GitConfig{
						Repository: "",
					},
				},
			},
			setupMocks: func(_ *MockGitClient) {
				// No mocks needed as validation should fail
			},
			expectError:   true,
			errorContains: "source validation failed",
		},
		{
			name: "clone failure",
			config: &config.Config{
				Source: config.SourceConfig{
					Format: config.SourceFormatToolHive,
					Type:   config.SourceTypeGit,
					Git: &config.GitConfig{
						Repository: testGitRepoURL,
					},
				},
			},
			setupMocks: func(gitClient *MockGitClient) {
				gitClient.On("Clone", mock.Anything, mock.Anything).Return(nil, errors.New("clone failed"))
			},
			expectError:   true,
			errorContains: "failed to clone repository",
		},
		{
			name: "file not found",
			config: &config.Config{
				Source: config.SourceConfig{
					Format: config.SourceFormatToolHive,
					Type:   config.SourceTypeGit,
					Git: &config.GitConfig{
						Repository: testGitRepoURL,
						Path:       testFilePath,
					},
				},
			},
			setupMocks: func(gitClient *MockGitClient) {
				repoInfo := &git.RepositoryInfo{
					RemoteURL: testGitRepoURL,
				}

				gitClient.On("Clone", mock.Anything, mock.Anything).Return(repoInfo, nil)
				gitClient.On("GetFileContent", repoInfo, testFilePath).Return(nil, errors.New("file not found"))
				gitClient.On("Cleanup", repoInfo).Return(nil)
			},
			expectError:   true,
			errorContains: "failed to get file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create mocks
			mockGitClient := new(MockGitClient)

			// Setup mocks
			tt.setupMocks(mockGitClient)

			// Create handler with mocks
			handler := &GitSourceHandler{
				gitClient: mockGitClient,
				validator: NewSourceDataValidator(), // Use real validator for hash tests
			}

			// Execute test
			hash, err := handler.CurrentHash(context.Background(), tt.config)

			// Verify results
			if tt.expectError {
				assert.Error(t, err)
				assert.Empty(t, hash)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, hash)
				if tt.expectedHash != "" {
					assert.Equal(t, tt.expectedHash, hash)
				}
			}

			// Verify all mock expectations
			mockGitClient.AssertExpectations(t)
		})
	}
}

func TestGitSourceHandler_DefaultPath(t *testing.T) {
	t.Parallel()

	handler := NewGitSourceHandler()

	// Test that default path is set when Path is empty
	source := &config.SourceConfig{
		Type: config.SourceTypeGit,
		Git: &config.GitConfig{
			Repository: testGitRepoURL,
		},
	}

	err := handler.Validate(source)
	require.NoError(t, err)
	assert.Equal(t, DefaultRegistryDataFile, source.Git.Path)
}

func TestGitSourceHandler_CleanupFailure(t *testing.T) {
	t.Parallel()

	// This test verifies that cleanup failures don't cause the operation to fail
	mockGitClient := new(MockGitClient)
	mockValidator := new(MockSourceDataValidator)

	registryConfig := &config.Config{
		Source: config.SourceConfig{
			Format: config.SourceFormatToolHive,
			Type:   config.SourceTypeGit,
			Git: &config.GitConfig{
				Repository: testGitRepoURL,
			},
		},
	}

	repoInfo := &git.RepositoryInfo{
		RemoteURL: testGitRepoURL,
	}
	testData := []byte(`{"version": "1.0.0"}`)
	testRegistry := &registry.Registry{
		Version:       "1.0.0",
		Servers:       make(map[string]*registry.ImageMetadata),
		RemoteServers: make(map[string]*registry.RemoteServerMetadata),
	}

	mockGitClient.On("Clone", mock.Anything, mock.Anything).Return(repoInfo, nil)
	mockGitClient.On("GetFileContent", repoInfo, DefaultRegistryDataFile).Return(testData, nil)
	mockGitClient.On("Cleanup", repoInfo).Return(errors.New("cleanup failed")) // Cleanup fails

	mockValidator.On("ValidateData", testData, config.SourceFormatToolHive).Return(testRegistry, nil)

	handler := &GitSourceHandler{
		gitClient: mockGitClient,
		validator: mockValidator,
	}

	// Despite cleanup failure, the operation should succeed
	result, err := handler.FetchRegistry(context.Background(), registryConfig)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	mockGitClient.AssertExpectations(t)
	mockValidator.AssertExpectations(t)
}
