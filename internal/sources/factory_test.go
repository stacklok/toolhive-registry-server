package sources

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestNewSourceHandlerFactory(t *testing.T) {
	t.Parallel()

	factory := NewSourceHandlerFactory()
	assert.NotNil(t, factory)
}

func TestDefaultSourceHandlerFactory_CreateHandler(t *testing.T) {
	t.Parallel()

	factory := NewSourceHandlerFactory()

	tests := []struct {
		name          string
		sourceType    string
		expectError   bool
		expectedType  interface{}
		errorContains string
	}{
		{
			name:         "file source type",
			sourceType:   config.SourceTypeFile,
			expectError:  false,
			expectedType: &fileSourceHandler{},
		},
		{
			name:         "git source type",
			sourceType:   config.SourceTypeGit,
			expectError:  false,
			expectedType: &gitSourceHandler{},
		},
		{
			name:         "api source type",
			sourceType:   config.SourceTypeAPI,
			expectError:  false,
			expectedType: &apiSourceHandler{},
		},
		{
			name:          "unsupported source type",
			sourceType:    "unsupported",
			expectError:   true,
			errorContains: "unsupported source type",
		},
		{
			name:          "empty source type",
			sourceType:    "",
			expectError:   true,
			errorContains: "unsupported source type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler, err := factory.CreateHandler(tt.sourceType)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, handler)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, handler)
				assert.IsType(t, tt.expectedType, handler)
			}
		})
	}
}
