package app

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/pkg/config"
	"github.com/stacklok/toolhive-registry-server/pkg/sources"
	"github.com/stacklok/toolhive-registry-server/pkg/status"
	pkgsync "github.com/stacklok/toolhive-registry-server/pkg/sync"
)

func TestNewRegistryAppBuilder(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		RegistryName: "test-registry",
		Source: config.SourceConfig{
			Type:   config.SourceTypeFile,
			Format: config.SourceFormatToolHive,
			File: &config.FileConfig{
				Path: "/tmp/test-registry.json",
			},
		},
		SyncPolicy: &config.SyncPolicyConfig{
			Interval: "30m",
		},
	}

	built, err := baseConfig(WithConfig(cfg))
	require.NoError(t, err)
	require.NotNil(t, built)
	assert.Equal(t, defaultHTTPAddress, built.address)
	assert.Equal(t, defaultDataDir, built.dataDir)
}

func TestRegistryAppWithFunctions(t *testing.T) {
	t.Parallel()
	built, err := baseConfig(
		WithConfig(createValidTestConfig()),
		WithAddress(":9090"),
	)
	require.NoError(t, err)
	require.NotNil(t, built)
}

func TestRegistryAppBuilder_WithAddress(t *testing.T) {
	t.Parallel()
	built, err := baseConfig(
		WithConfig(createValidTestConfig()),
		WithAddress(":9090"),
	)
	require.NoError(t, err)
	assert.Equal(t, ":9090", built.address)
}

func TestRegistryAppBuilder_ChainedBuilder(t *testing.T) {
	t.Parallel()
	cfg := createValidTestConfig()

	built, err := baseConfig(
		WithConfig(cfg),
		WithAddress(":8888"),
		WithDataDirectory("/tmp/test-data"),
	)
	require.NoError(t, err)
	require.NotNil(t, built)
	assert.Equal(t, ":8888", built.address)
	assert.Equal(t, "/tmp/test-data", built.dataDir)
	assert.Equal(t, "/tmp/test-data/registry.json", built.registryFile)
	assert.Equal(t, "/tmp/test-data/status.json", built.statusFile)
}

// createValidTestConfig creates a minimal valid config for testing
func createValidTestConfig() *config.Config {
	return &config.Config{
		RegistryName: "test-registry",
		Source: config.SourceConfig{
			Type:   config.SourceTypeFile,
			Format: config.SourceFormatToolHive,
			File: &config.FileConfig{
				Path: "/tmp/test-registry.json",
			},
		},
		SyncPolicy: &config.SyncPolicyConfig{
			Interval: "30m",
		},
	}
}

func TestWithConfig(t *testing.T) {
	t.Parallel()
	cfg := &registryAppConfig{}
	testConfig := createValidTestConfig()

	opt := WithConfig(testConfig)
	err := opt(cfg)

	require.NoError(t, err)
	assert.Equal(t, testConfig, cfg.config)
}

func TestWithAddress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		address string
		want    string
	}{
		{name: "valid_address", address: ":9999", want: ":9999"},
		{name: "valid_address_with_host", address: "127.0.0.1:9999", want: "127.0.0.1:9999"},
		{name: "valid_address_with_host_and_port", address: "localhost:9999", want: "localhost:9999"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &registryAppConfig{}
			opt := WithAddress(tt.address)
			err := opt(cfg)
			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.address)
		})
	}
}

func TestWithMiddlewares(t *testing.T) {
	t.Parallel()
	cfg := &registryAppConfig{}
	middleware1 := func(next http.Handler) http.Handler { return next }
	middleware2 := func(next http.Handler) http.Handler { return next }

	opt := WithMiddlewares(middleware1, middleware2)
	err := opt(cfg)

	require.NoError(t, err)
	assert.Len(t, cfg.middlewares, 2)
}

func TestWithSourceHandlerFactory(t *testing.T) {
	t.Parallel()
	cfg := &registryAppConfig{}
	// Use nil factory for testing - we're just verifying the field is set
	var testFactory sources.SourceHandlerFactory

	opt := WithSourceHandlerFactory(testFactory)
	err := opt(cfg)

	require.NoError(t, err)
	assert.Equal(t, testFactory, cfg.sourceHandlerFactory)
}

func TestWithStorageManager(t *testing.T) {
	t.Parallel()
	cfg := &registryAppConfig{}
	// Use nil storage manager for testing - we're just verifying the field is set
	var testStorageManager sources.StorageManager

	opt := WithStorageManager(testStorageManager)
	err := opt(cfg)

	require.NoError(t, err)
	assert.Equal(t, testStorageManager, cfg.storageManager)
}

func TestWithStatusPersistence(t *testing.T) {
	t.Parallel()
	cfg := &registryAppConfig{}
	// Use nil status persistence for testing - we're just verifying the field is set
	var testStatusPersistence status.StatusPersistence

	opt := WithStatusPersistence(testStatusPersistence)
	err := opt(cfg)

	require.NoError(t, err)
	assert.Equal(t, testStatusPersistence, cfg.statusPersistence)
}

func TestWithSyncManager(t *testing.T) {
	t.Parallel()
	cfg := &registryAppConfig{}
	// Use nil sync manager for testing - we're just verifying the field is set
	var testSyncManager pkgsync.Manager

	opt := WithSyncManager(testSyncManager)
	err := opt(cfg)

	require.NoError(t, err)
	assert.Equal(t, testSyncManager, cfg.syncManager)
}

func TestWithRegistryProvider(t *testing.T) {
	t.Parallel()
	cfg := &registryAppConfig{}
	// Use nil registry provider for testing - we're just verifying the field is set
	var testRegistryProvider service.RegistryDataProvider

	opt := WithRegistryProvider(testRegistryProvider)
	err := opt(cfg)

	require.NoError(t, err)
	assert.Equal(t, testRegistryProvider, cfg.registryProvider)
}

func TestWithDeploymentProvider(t *testing.T) {
	t.Parallel()
	cfg := &registryAppConfig{}
	// Use nil deployment provider for testing - we're just verifying the field is set
	var testDeploymentProvider service.DeploymentProvider

	opt := WithDeploymentProvider(testDeploymentProvider)
	err := opt(cfg)

	require.NoError(t, err)
	assert.Equal(t, testDeploymentProvider, cfg.deploymentProvider)
}
