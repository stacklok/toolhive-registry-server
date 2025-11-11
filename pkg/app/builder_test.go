package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/pkg/config"
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

	builder := NewRegistryAppBuilder(cfg)
	require.NotNil(t, builder)
	assert.Equal(t, cfg, builder.config)
	assert.Equal(t, defaultHTTPAddress, builder.address)
	assert.Equal(t, defaultDataDir, builder.dataDir)
}

func TestRegistryAppBuilder_WithAddress(t *testing.T) {
	t.Parallel()
	cfg := createValidTestConfig()
	builder := NewRegistryAppBuilder(cfg).WithAddress(":9090")

	assert.Equal(t, ":9090", builder.address)
}

func TestRegistryAppBuilder_WithDataDirectory(t *testing.T) {
	t.Parallel()
	cfg := createValidTestConfig()
	builder := NewRegistryAppBuilder(cfg).WithDataDirectory("/custom/data")

	assert.Equal(t, "/custom/data", builder.dataDir)
	assert.Equal(t, "/custom/data/registry.json", builder.registryFile)
	assert.Equal(t, "/custom/data/status.json", builder.statusFile)
}

func TestRegistryAppBuilder_Build_InvalidConfig(t *testing.T) {
	t.Parallel()
	// Config with no source type
	cfg := &config.Config{
		SyncPolicy: &config.SyncPolicyConfig{
			Interval: "30m",
		},
	}

	builder := NewRegistryAppBuilder(cfg)
	_, err := builder.Build(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid configuration")
}

func TestRegistryAppBuilder_ChainedBuilder(t *testing.T) {
	t.Parallel()
	cfg := createValidTestConfig()

	builder := NewRegistryAppBuilder(cfg).
		WithAddress(":8888").
		WithDataDirectory("/tmp/test-data")

	require.NotNil(t, builder)
	assert.Equal(t, ":8888", builder.address)
	assert.Equal(t, "/tmp/test-data", builder.dataDir)
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
