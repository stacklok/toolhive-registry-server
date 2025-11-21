package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFileRegistryDataProvider_GetRegistryName(t *testing.T) {
	t.Parallel()

	provider := &fileRegistryDataProvider{
		registryName: "test-registry",
	}

	assert.Equal(t, "test-registry", provider.GetRegistryName())
}

// TODO: Update file provider tests for multi-registry architecture
// The tests need to be updated to use the new Config structure with Registries array
