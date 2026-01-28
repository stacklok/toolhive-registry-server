package sources

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"testing"

	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/git"
	"github.com/stacklok/toolhive-registry-server/internal/registry"
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

// MockRegistryDataValidator is a mock implementation of RegistryDataValidator
type MockRegistryDataValidator struct {
	mock.Mock
}

func (m *MockRegistryDataValidator) ValidateData(data []byte, format string) (*toolhivetypes.UpstreamRegistry, error) {
	args := m.Called(data, format)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*toolhivetypes.UpstreamRegistry), args.Error(1)
}

func TestNewGitRegistryHandler(t *testing.T) {
	t.Parallel()

	handler := NewGitRegistryHandler()

	assert.NotNil(t, handler)
	// Cast to concrete type to access fields in tests (same package)
	concreteHandler := handler.(*gitRegistryHandler)
	assert.NotNil(t, concreteHandler.gitClient)
	assert.NotNil(t, concreteHandler.validator)
}

func TestGitRegistryHandler_Validate(t *testing.T) {
	t.Parallel()

	handler := NewGitRegistryHandler()

	tests := []struct {
		name           string
		registryConfig *config.RegistryConfig
		expectError    bool
		errorContains  string
	}{
		{
			name: "valid git config with repository only",
			registryConfig: &config.RegistryConfig{
				Name: "test-git",
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
				},
			},
			expectError: false,
		},
		{
			name: "valid git config with branch",
			registryConfig: &config.RegistryConfig{
				Name: "test-git",
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
					Branch:     testBranch,
				},
			},
			expectError: false,
		},
		{
			name: "valid git config with tag",
			registryConfig: &config.RegistryConfig{
				Name: "test-git",
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
					Tag:        testTag,
				},
			},
			expectError: false,
		},
		{
			name: "valid git config with commit",
			registryConfig: &config.RegistryConfig{
				Name: "test-git",
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
					Commit:     testCommit,
				},
			},
			expectError: false,
		},
		{
			name: "valid git config with custom path",
			registryConfig: &config.RegistryConfig{
				Name: "test-git",
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
					Path:       testFilePath,
				},
			},
			expectError: false,
		},
		{
			name:           "nil registry config",
			registryConfig: nil,
			expectError:    true,
			errorContains:  "registry configuration cannot be nil",
		},
		{
			name: "missing git configuration",
			registryConfig: &config.RegistryConfig{
				Name: "test-git",
				Git:  nil,
			},
			expectError:   true,
			errorContains: "git configuration is required",
		},
		{
			name: "empty repository URL",
			registryConfig: &config.RegistryConfig{
				Name: "test-git",
				Git: &config.GitConfig{
					Repository: "",
				},
			},
			expectError:   true,
			errorContains: "git repository URL cannot be empty",
		},
		{
			name: "multiple reference types - branch and tag",
			registryConfig: &config.RegistryConfig{
				Name: "test-git",
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
					Branch:     testBranch,
					Tag:        testTag,
				},
			},
			expectError:   true,
			errorContains: "only one of branch, tag, or commit may be specified",
		},
		{
			name: "multiple reference types - branch and commit",
			registryConfig: &config.RegistryConfig{
				Name: "test-git",
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
					Branch:     testBranch,
					Commit:     testCommit,
				},
			},
			expectError:   true,
			errorContains: "only one of branch, tag, or commit may be specified",
		},
		{
			name: "multiple reference types - tag and commit",
			registryConfig: &config.RegistryConfig{
				Name: "test-git",
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
					Tag:        testTag,
					Commit:     testCommit,
				},
			},
			expectError:   true,
			errorContains: "only one of branch, tag, or commit may be specified",
		},
		{
			name: "all three reference types",
			registryConfig: &config.RegistryConfig{
				Name: "test-git",
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
					Branch:     testBranch,
					Tag:        testTag,
					Commit:     testCommit,
				},
			},
			expectError:   true,
			errorContains: "only one of branch, tag, or commit may be specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := handler.Validate(tt.registryConfig)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				assert.NoError(t, err)
				// Check that default path is set when not specified
				if tt.registryConfig.Git != nil && tt.registryConfig.Git.Path == "" {
					assert.Equal(t, DefaultRegistryDataFile, tt.registryConfig.Git.Path)
				}
			}
		})
	}
}

func TestGitRegistryHandler_FetchRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		registryConfig *config.RegistryConfig
		setupMocks     func(*MockGitClient, *MockRegistryDataValidator)
		expectError    bool
		errorContains  string
	}{
		{
			name: "successful fetch with default path",
			registryConfig: &config.RegistryConfig{
				Name:   "test-git",
				Format: config.SourceFormatToolHive,
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
					Branch:     testBranch,
				},
			},
			setupMocks: func(gitClient *MockGitClient, validator *MockRegistryDataValidator) {
				repoInfo := &git.RepositoryInfo{
					RemoteURL: testGitRepoURL,
				}
				testData := []byte(`{"version": "1.0.0"}`)

				// Create UpstreamRegistry directly
				upstreamRegistry := registry.NewTestUpstreamRegistry(
					registry.WithVersion("1.0.0"),
				)

				gitClient.On("Clone", mock.Anything, mock.MatchedBy(func(cfg *git.CloneConfig) bool {
					return cfg.URL == testGitRepoURL && cfg.Branch == testBranch
				})).Return(repoInfo, nil)

				gitClient.On("GetFileContent", repoInfo, DefaultRegistryDataFile).Return(testData, nil)
				gitClient.On("Cleanup", repoInfo).Return(nil)

				validator.On("ValidateData", testData, config.SourceFormatToolHive).Return(upstreamRegistry, nil)
			},
			expectError: false,
		},
		{
			name: "successful fetch with custom path",
			registryConfig: &config.RegistryConfig{
				Name:   "test-git",
				Format: config.SourceFormatToolHive,
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
					Tag:        testTag,
					Path:       testFilePath,
				},
			},
			setupMocks: func(gitClient *MockGitClient, validator *MockRegistryDataValidator) {
				repoInfo := &git.RepositoryInfo{
					RemoteURL: testGitRepoURL,
				}
				testData := []byte(`{"version": "1.0.0"}`)

				// Create UpstreamRegistry directly
				upstreamRegistry := registry.NewTestUpstreamRegistry(
					registry.WithVersion("1.0.0"),
				)

				gitClient.On("Clone", mock.Anything, mock.MatchedBy(func(cfg *git.CloneConfig) bool {
					return cfg.URL == testGitRepoURL && cfg.Tag == testTag
				})).Return(repoInfo, nil)

				gitClient.On("GetFileContent", repoInfo, testFilePath).Return(testData, nil)
				gitClient.On("Cleanup", repoInfo).Return(nil)

				validator.On("ValidateData", testData, config.SourceFormatToolHive).Return(upstreamRegistry, nil)
			},
			expectError: false,
		},
		{
			name: "validation failure",
			registryConfig: &config.RegistryConfig{
				Name:   "test-git",
				Format: config.SourceFormatToolHive,
				Git: &config.GitConfig{
					Repository: "", // Invalid
				},
			},
			setupMocks: func(_ *MockGitClient, _ *MockRegistryDataValidator) {
				// No mocks needed as validation should fail before git operations
			},
			expectError:   true,
			errorContains: "registry validation failed",
		},
		{
			name: "clone failure",
			registryConfig: &config.RegistryConfig{
				Name:   "test-git",
				Format: config.SourceFormatToolHive,
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
					Commit:     testCommit,
				},
			},
			setupMocks: func(gitClient *MockGitClient, _ *MockRegistryDataValidator) {
				gitClient.On("Clone", mock.Anything, mock.MatchedBy(func(cfg *git.CloneConfig) bool {
					return cfg.URL == testGitRepoURL && cfg.Commit == testCommit
				})).Return(nil, errors.New("clone failed"))
			},
			expectError:   true,
			errorContains: "failed to clone repository",
		},
		{
			name: "file not found",
			registryConfig: &config.RegistryConfig{
				Name:   "test-git",
				Format: config.SourceFormatToolHive,
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
				},
			},
			setupMocks: func(gitClient *MockGitClient, _ *MockRegistryDataValidator) {
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
			registryConfig: &config.RegistryConfig{
				Name:   "test-git",
				Format: config.SourceFormatToolHive,
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
				},
			},
			setupMocks: func(gitClient *MockGitClient, validator *MockRegistryDataValidator) {
				repoInfo := &git.RepositoryInfo{
					RemoteURL: testGitRepoURL,
				}
				testData := []byte(`invalid json`)

				gitClient.On("Clone", mock.Anything, mock.Anything).Return(repoInfo, nil)
				gitClient.On("GetFileContent", repoInfo, DefaultRegistryDataFile).Return(testData, nil)
				gitClient.On("Cleanup", repoInfo).Return(nil)

				validator.On("ValidateData", testData, config.SourceFormatToolHive).Return((*toolhivetypes.UpstreamRegistry)(nil), errors.New("invalid data"))
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
			mockValidator := new(MockRegistryDataValidator)

			// Setup mocks
			tt.setupMocks(mockGitClient, mockValidator)

			// Create handler with mocks
			handler := &gitRegistryHandler{
				gitClient: mockGitClient,
				validator: mockValidator,
			}

			// Execute test
			result, err := handler.FetchRegistry(context.Background(), tt.registryConfig)

			// Verify results
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, result)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.NotEmpty(t, result.Hash)
				assert.Equal(t, tt.registryConfig.Format, result.Format)
			}

			// Verify all mock expectations
			mockGitClient.AssertExpectations(t)
			mockValidator.AssertExpectations(t)
		})
	}
}

func TestGitRegistryHandler_CurrentHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		registryConfig *config.RegistryConfig
		setupMocks     func(*MockGitClient)
		expectError    bool
		errorContains  string
		expectedHash   string
	}{
		{
			name: "successful hash calculation",
			registryConfig: &config.RegistryConfig{
				Name:   "test-git",
				Format: config.SourceFormatToolHive,
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
					Branch:     testBranch,
				},
			},
			setupMocks: func(gitClient *MockGitClient) {
				repoInfo := &git.RepositoryInfo{
					RemoteURL: testGitRepoURL,
				}
				testData := []byte(`{"version": "1.0.0"}`)

				gitClient.On("Clone", mock.Anything, mock.MatchedBy(func(cfg *git.CloneConfig) bool {
					return cfg.URL == testGitRepoURL && cfg.Branch == testBranch
				})).Return(repoInfo, nil)

				gitClient.On("GetFileContent", repoInfo, DefaultRegistryDataFile).Return(testData, nil)
				gitClient.On("Cleanup", repoInfo).Return(nil)
			},
			expectError:  false,
			expectedHash: fmt.Sprintf("%x", sha256.Sum256([]byte(`{"version": "1.0.0"}`))),
		},
		{
			name: "validation failure",
			registryConfig: &config.RegistryConfig{
				Name:   "test-git",
				Format: config.SourceFormatToolHive,
				Git: &config.GitConfig{
					Repository: "",
				},
			},
			setupMocks: func(_ *MockGitClient) {
				// No mocks needed as validation should fail
			},
			expectError:   true,
			errorContains: "registry validation failed",
		},
		{
			name: "clone failure",
			registryConfig: &config.RegistryConfig{
				Name:   "test-git",
				Format: config.SourceFormatToolHive,
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
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
			registryConfig: &config.RegistryConfig{
				Name:   "test-git",
				Format: config.SourceFormatToolHive,
				Git: &config.GitConfig{
					Repository: testGitRepoURL,
					Path:       testFilePath,
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
			handler := &gitRegistryHandler{
				gitClient: mockGitClient,
				validator: NewRegistryDataValidator(), // Use real validator for hash tests
			}

			// Execute test
			hash, err := handler.CurrentHash(context.Background(), tt.registryConfig)

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

func TestGitRegistryHandler_DefaultPath(t *testing.T) {
	t.Parallel()

	// Test that default path is set during validation
	registryConfig := &config.RegistryConfig{
		Name: "test-git",
		Git: &config.GitConfig{
			Repository: testGitRepoURL,
			// Path not set - should be set to default
		},
	}

	handler := NewGitRegistryHandler()
	err := handler.Validate(registryConfig)

	require.NoError(t, err)
	assert.Equal(t, DefaultRegistryDataFile, registryConfig.Git.Path, "Default path should be set during validation")
}

func TestGitRegistryHandler_CleanupFailure(t *testing.T) {
	t.Parallel()

	// Test that cleanup failure doesn't prevent successful fetch
	registryConfig := &config.RegistryConfig{
		Name:   "test-git",
		Format: config.SourceFormatToolHive,
		Git: &config.GitConfig{
			Repository: testGitRepoURL,
			Branch:     testBranch,
		},
	}

	mockGitClient := new(MockGitClient)
	mockValidator := new(MockRegistryDataValidator)

	repoInfo := &git.RepositoryInfo{
		RemoteURL: testGitRepoURL,
	}
	testData := []byte(`{"version": "1.0.0"}`)
	upstreamRegistry := registry.NewTestUpstreamRegistry(
		registry.WithVersion("1.0.0"),
	)

	mockGitClient.On("Clone", mock.Anything, mock.Anything).Return(repoInfo, nil)
	mockGitClient.On("GetFileContent", repoInfo, DefaultRegistryDataFile).Return(testData, nil)
	mockGitClient.On("Cleanup", repoInfo).Return(errors.New("cleanup failed")) // Cleanup fails

	mockValidator.On("ValidateData", testData, config.SourceFormatToolHive).Return(upstreamRegistry, nil)

	handler := &gitRegistryHandler{
		gitClient: mockGitClient,
		validator: mockValidator,
	}

	// Should still succeed despite cleanup failure
	result, err := handler.FetchRegistry(context.Background(), registryConfig)

	assert.NoError(t, err, "Fetch should succeed even if cleanup fails")
	assert.NotNil(t, result)

	mockGitClient.AssertExpectations(t)
	mockValidator.AssertExpectations(t)
}

func TestGitRegistryHandler_FetchRegistryWithAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		setupAuth      func(t *testing.T) *config.GitAuthConfig
		registryConfig func(auth *config.GitAuthConfig) *config.RegistryConfig
		setupMocks     func(*MockGitClient, *MockRegistryDataValidator)
		expectError    bool
		errorContains  string
	}{
		{
			name: "successful fetch with authentication",
			setupAuth: func(t *testing.T) *config.GitAuthConfig {
				t.Helper()
				// Create a temp password file
				tmpDir := t.TempDir()
				passwordFile := tmpDir + "/password.txt"
				err := os.WriteFile(passwordFile, []byte("test-token"), 0600)
				require.NoError(t, err)

				return &config.GitAuthConfig{
					Username:     "testuser",
					PasswordFile: passwordFile,
				}
			},
			registryConfig: func(auth *config.GitAuthConfig) *config.RegistryConfig {
				return &config.RegistryConfig{
					Name:   "test-git-auth",
					Format: config.SourceFormatToolHive,
					Git: &config.GitConfig{
						Repository: testGitRepoURL,
						Branch:     testBranch,
						Auth:       auth,
					},
				}
			},
			setupMocks: func(gitClient *MockGitClient, validator *MockRegistryDataValidator) {
				repoInfo := &git.RepositoryInfo{
					RemoteURL: testGitRepoURL,
				}
				testData := []byte(`{"version": "1.0.0"}`)

				upstreamRegistry := registry.NewTestUpstreamRegistry(
					registry.WithVersion("1.0.0"),
				)

				// Verify that auth credentials are correctly passed to the git client
				gitClient.On("Clone", mock.Anything, mock.MatchedBy(func(cfg *git.CloneConfig) bool {
					return cfg.URL == testGitRepoURL &&
						cfg.Branch == testBranch &&
						cfg.Auth != nil &&
						cfg.Auth.Username == "testuser" &&
						cfg.Auth.Password == "test-token"
				})).Return(repoInfo, nil)

				gitClient.On("GetFileContent", repoInfo, DefaultRegistryDataFile).Return(testData, nil)
				gitClient.On("Cleanup", repoInfo).Return(nil)

				validator.On("ValidateData", testData, config.SourceFormatToolHive).Return(upstreamRegistry, nil)
			},
			expectError: false,
		},
		{
			name: "fetch fails when password file not readable",
			setupAuth: func(t *testing.T) *config.GitAuthConfig {
				t.Helper()
				// Use a path that doesn't exist
				tmpDir := t.TempDir()
				nonExistentFile := tmpDir + "/nonexistent-password.txt"

				return &config.GitAuthConfig{
					Username:     "testuser",
					PasswordFile: nonExistentFile,
				}
			},
			registryConfig: func(auth *config.GitAuthConfig) *config.RegistryConfig {
				return &config.RegistryConfig{
					Name:   "test-git-auth-fail",
					Format: config.SourceFormatToolHive,
					Git: &config.GitConfig{
						Repository: testGitRepoURL,
						Branch:     testBranch,
						Auth:       auth,
					},
				}
			},
			setupMocks: func(_ *MockGitClient, _ *MockRegistryDataValidator) {
				// No mocks needed as the fetch should fail before git operations
			},
			expectError:   true,
			errorContains: "failed to get git password",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup auth config (may create temp files)
			authConfig := tt.setupAuth(t)

			// Create registry config with auth
			registryConfig := tt.registryConfig(authConfig)

			// Create mocks
			mockGitClient := new(MockGitClient)
			mockValidator := new(MockRegistryDataValidator)

			// Setup mocks
			tt.setupMocks(mockGitClient, mockValidator)

			// Create handler with mocks
			handler := &gitRegistryHandler{
				gitClient: mockGitClient,
				validator: mockValidator,
			}

			// Execute test
			result, err := handler.FetchRegistry(context.Background(), registryConfig)

			// Verify results
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, result)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.NotEmpty(t, result.Hash)
				assert.Equal(t, registryConfig.Format, result.Format)
			}

			// Verify all mock expectations
			mockGitClient.AssertExpectations(t)
			mockValidator.AssertExpectations(t)
		})
	}
}
