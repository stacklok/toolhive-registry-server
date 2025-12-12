package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestNewFileFactory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     *config.Config
		dataDir string
		setup   func(*testing.T) (string, string) // returns baseDir, dataDir
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config with writable directory",
			cfg: &config.Config{
				RegistryName: "test-registry",
			},
			setup: func(t *testing.T) (string, string) {
				t.Helper()
				baseDir := t.TempDir()
				dataDir := filepath.Join(baseDir, "data")
				return baseDir, dataDir
			},
			wantErr: false,
		},
		{
			name:    "nil config returns error",
			cfg:     nil,
			wantErr: true,
			errMsg:  "config cannot be nil",
		},
		{
			name: "non-existent directory is created",
			cfg: &config.Config{
				RegistryName: "test-registry",
			},
			setup: func(t *testing.T) (string, string) {
				t.Helper()
				baseDir := filepath.Join(t.TempDir(), "new", "nested", "dir")
				dataDir := filepath.Join(baseDir, "data")
				return baseDir, dataDir
			},
			wantErr: false,
		},
		{
			name: "valid config with explicit base directory",
			cfg: &config.Config{
				RegistryName: "test-registry",
				FileStorage: &config.FileStorageConfig{
					BaseDir: "", // Will be set in setup
				},
			},
			setup: func(t *testing.T) (string, string) {
				t.Helper()
				baseDir := t.TempDir()
				dataDir := filepath.Join(baseDir, "custom-data")
				return baseDir, dataDir
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var dataDir string
			if tt.setup != nil {
				baseDir, tmpDataDir := tt.setup(t)
				dataDir = tmpDataDir
				if tt.cfg != nil && tt.cfg.FileStorage != nil {
					tt.cfg.FileStorage.BaseDir = baseDir
				}
			}

			factory, err := NewFileFactory(tt.cfg, dataDir)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, factory)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, factory)
			assert.NotNil(t, factory.config)
			assert.Equal(t, tt.cfg, factory.config)
			assert.Equal(t, dataDir, factory.dataDir)
			assert.NotNil(t, factory.storageManager)
			assert.NotNil(t, factory.statusPersistence)

			// Verify directory was created
			baseDir := tt.cfg.GetFileStorageBaseDir()
			info, err := os.Stat(baseDir)
			require.NoError(t, err)
			assert.True(t, info.IsDir())
		})
	}
}

func TestNewFileFactory_ReadOnlyFilesystem(t *testing.T) {
	t.Parallel()

	// This test simulates a read-only filesystem error by using an invalid path
	// On most systems, trying to create a directory in the root without permissions will fail
	tests := []struct {
		name    string
		cfg     *config.Config
		dataDir string
		wantErr bool
		errMsg  string
	}{
		{
			name: "read-only or permission denied directory",
			cfg: &config.Config{
				RegistryName: "test-registry",
				FileStorage: &config.FileStorageConfig{
					BaseDir: "/root/nonexistent/readonly/path",
				},
			},
			dataDir: "/root/nonexistent/readonly/path/data",
			wantErr: true,
			errMsg:  "failed to create data directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			factory, err := NewFileFactory(tt.cfg, tt.dataDir)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, factory)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, factory)
		})
	}
}

func TestFileFactory_CreateStateService(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func(*testing.T) *FileFactory
		wantErr bool
	}{
		{
			name: "successfully creates state service",
			setup: func(t *testing.T) *FileFactory {
				t.Helper()
				baseDir := t.TempDir()
				dataDir := filepath.Join(baseDir, "data")
				cfg := &config.Config{
					RegistryName: "test-registry",
					FileStorage: &config.FileStorageConfig{
						BaseDir: baseDir,
					},
				}
				factory, err := NewFileFactory(cfg, dataDir)
				require.NoError(t, err)
				return factory
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			factory := tt.setup(t)
			stateService, err := factory.CreateStateService(ctx)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, stateService)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, stateService)
		})
	}
}

func TestFileFactory_CreateSyncWriter(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func(*testing.T) *FileFactory
		wantErr bool
	}{
		{
			name: "successfully creates sync writer",
			setup: func(t *testing.T) *FileFactory {
				t.Helper()
				baseDir := t.TempDir()
				dataDir := filepath.Join(baseDir, "data")
				cfg := &config.Config{
					RegistryName: "test-registry",
					FileStorage: &config.FileStorageConfig{
						BaseDir: baseDir,
					},
				}
				factory, err := NewFileFactory(cfg, dataDir)
				require.NoError(t, err)
				return factory
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			factory := tt.setup(t)
			syncWriter, err := factory.CreateSyncWriter(ctx)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, syncWriter)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, syncWriter)
		})
	}
}

func TestFileFactory_CreateRegistryService(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name    string
		setup   func(*testing.T) *FileFactory
		wantErr bool
	}{
		{
			name: "successfully creates registry service",
			setup: func(t *testing.T) *FileFactory {
				t.Helper()
				baseDir := t.TempDir()
				dataDir := filepath.Join(baseDir, "data")
				cfg := &config.Config{
					RegistryName: "test-registry",
					FileStorage: &config.FileStorageConfig{
						BaseDir: baseDir,
					},
				}
				factory, err := NewFileFactory(cfg, dataDir)
				require.NoError(t, err)
				return factory
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			factory := tt.setup(t)
			registryService, err := factory.CreateRegistryService(ctx)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, registryService)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, registryService)
		})
	}
}

func TestFileFactory_Cleanup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(*testing.T) *FileFactory
	}{
		{
			name: "cleanup is a no-op and does not panic",
			setup: func(t *testing.T) *FileFactory {
				t.Helper()
				baseDir := t.TempDir()
				dataDir := filepath.Join(baseDir, "data")
				cfg := &config.Config{
					RegistryName: "test-registry",
					FileStorage: &config.FileStorageConfig{
						BaseDir: baseDir,
					},
				}
				factory, err := NewFileFactory(cfg, dataDir)
				require.NoError(t, err)
				return factory
			},
		},
		{
			name: "cleanup can be called multiple times (idempotent)",
			setup: func(t *testing.T) *FileFactory {
				t.Helper()
				baseDir := t.TempDir()
				dataDir := filepath.Join(baseDir, "data")
				cfg := &config.Config{
					RegistryName: "test-registry",
					FileStorage: &config.FileStorageConfig{
						BaseDir: baseDir,
					},
				}
				factory, err := NewFileFactory(cfg, dataDir)
				require.NoError(t, err)
				return factory
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			factory := tt.setup(t)

			// Should not panic
			require.NotPanics(t, func() {
				factory.Cleanup()
			})

			// For idempotency test, call cleanup again
			if tt.name == "cleanup can be called multiple times (idempotent)" {
				require.NotPanics(t, func() {
					factory.Cleanup()
				})
			}
		})
	}
}
