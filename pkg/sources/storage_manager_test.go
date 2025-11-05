package sources

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewConfigMapStorageManager(t *testing.T) {
	t.Parallel()

	manager, err := NewStorageManager()
	require.Error(t, err)
	require.Nil(t, manager)
}
