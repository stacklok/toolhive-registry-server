package sources

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewFileRegistryHandler(t *testing.T) {
	t.Parallel()

	handler := NewFileRegistryHandler()
	assert.NotNil(t, handler, "NewFileRegistryHandler should return a non-nil handler")
}

// TODO: Update file registry handler tests for multi-registry architecture
// The tests need to be updated to use RegistryConfig instead of Config with SourceConfig
