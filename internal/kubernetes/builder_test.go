package kubernetes

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	toolhivetypes "github.com/stacklok/toolhive/pkg/registry/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/sync/writer"
)

// mockSyncWriter is a simple mock implementation of writer.SyncWriter for testing
type mockSyncWriter struct{}

func (*mockSyncWriter) Store(_ context.Context, _ string, _ *toolhivetypes.UpstreamRegistry) error {
	return nil
}

func TestWithNamespaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		namespaces []string
		initial    *mcpServerReconcilerOptions
		want       *[]string
	}{
		{
			name:       "single namespace",
			namespaces: []string{"default"},
			initial:    &mcpServerReconcilerOptions{},
			want:       &[]string{"default"},
		},
		{
			name:       "multiple namespaces",
			namespaces: []string{"default", "kube-system", "production"},
			initial:    &mcpServerReconcilerOptions{},
			want:       &[]string{"default", "kube-system", "production"},
		},
		{
			name:       "empty namespaces",
			namespaces: []string{},
			initial:    &mcpServerReconcilerOptions{},
			want:       &[]string{},
		},
		{
			name:       "nil initial namespaces",
			namespaces: []string{"default"},
			initial:    &mcpServerReconcilerOptions{namespaces: nil},
			want:       &[]string{"default"},
		},
		{
			name:       "append to existing namespaces",
			namespaces: []string{"new-namespace"},
			initial:    &mcpServerReconcilerOptions{namespaces: []string{"existing"}},
			want:       &[]string{"existing", "new-namespace"},
		},
		{
			name:       "multiple appends",
			namespaces: []string{"ns1", "ns2", "ns3"},
			initial:    &mcpServerReconcilerOptions{namespaces: []string{"existing"}},
			want:       &[]string{"existing", "ns1", "ns2", "ns3"},
		},
		{
			name:       "invalid namespace returns error",
			namespaces: []string{"Invalid-Namespace"},
			initial:    &mcpServerReconcilerOptions{},
			want:       nil,
		},
		{
			name:       "duplicate namespace returns error",
			namespaces: []string{"default", "default"},
			initial:    &mcpServerReconcilerOptions{},
			want:       nil,
		},
		{
			name:       "duplicate namespace not adjacent returns error",
			namespaces: []string{"a", "b", "a"},
			initial:    &mcpServerReconcilerOptions{},
			want:       nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opt := WithNamespaces(tt.namespaces...)
			err := opt(tt.initial)

			if tt.want == nil {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, *tt.want, tt.initial.namespaces)
		})
	}
}

// TestWithCurrentNamespace tests the WithCurrentNamespace option.
// Note: Subtests cannot use t.Parallel() because they modify the shared
// package-level variable serviceAccountNamespaceFile, which would cause data races.
//
//nolint:tparallel // Subtests modify shared package-level state
func TestWithCurrentNamespace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		content    string
		initial    *mcpServerReconcilerOptions
		want       []string
		skipCreate bool
	}{
		{
			name:    "single namespace from file",
			content: "test-namespace",
			initial: &mcpServerReconcilerOptions{},
			want:    []string{"test-namespace"},
		},
		{
			name:    "namespace with trailing newline",
			content: "test-namespace\n",
			initial: &mcpServerReconcilerOptions{},
			want:    []string{"test-namespace"},
		},
		{
			name:    "nil initial namespaces",
			content: "default",
			initial: &mcpServerReconcilerOptions{namespaces: nil},
			want:    []string{"default"},
		},
		{
			name:    "append to existing namespaces",
			content: "current-namespace",
			initial: &mcpServerReconcilerOptions{namespaces: []string{"existing"}},
			want:    []string{"existing", "current-namespace"},
		},
		{
			name:       "returns error when namespace file does not exist",
			initial:    &mcpServerReconcilerOptions{},
			skipCreate: true,
		},
	}

	//nolint:paralleltest // Subtests modify shared package-level variable serviceAccountNamespaceFile
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tmpFile := filepath.Join(tmpDir, "namespace")

			if !tt.skipCreate {
				err := os.WriteFile(tmpFile, []byte(tt.content), 0o600)
				require.NoError(t, err)
			}

			// Override the namespace file path for testing
			originalPath := serviceAccountNamespaceFile
			serviceAccountNamespaceFile = tmpFile
			t.Cleanup(func() {
				serviceAccountNamespaceFile = originalPath
			})

			opt := WithCurrentNamespace()
			err := opt(tt.initial)

			if tt.want == nil {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, tt.initial.namespaces)
		})
	}
}

func TestWithRequeueAfter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		requeueAfter time.Duration
		initial      *mcpServerReconcilerOptions
		want         time.Duration
		wantErr      bool
	}{
		{
			name:         "valid positive duration",
			requeueAfter: 5 * time.Second,
			initial:      &mcpServerReconcilerOptions{},
			want:         5 * time.Second,
			wantErr:      false,
		},
		{
			name:         "valid minute duration",
			requeueAfter: 2 * time.Minute,
			initial:      &mcpServerReconcilerOptions{},
			want:         2 * time.Minute,
			wantErr:      false,
		},
		{
			name:         "valid millisecond duration",
			requeueAfter: 100 * time.Millisecond,
			initial:      &mcpServerReconcilerOptions{},
			want:         100 * time.Millisecond,
			wantErr:      false,
		},
		{
			name:         "zero duration should error",
			requeueAfter: 0,
			initial:      &mcpServerReconcilerOptions{},
			want:         0,
			wantErr:      true,
		},
		{
			name:         "negative duration should error",
			requeueAfter: -1 * time.Second,
			initial:      &mcpServerReconcilerOptions{},
			want:         0,
			wantErr:      true,
		},
		{
			name:         "overwrite existing duration",
			requeueAfter: 10 * time.Second,
			initial:      &mcpServerReconcilerOptions{requeueAfter: 5 * time.Second},
			want:         10 * time.Second,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opt := WithRequeueAfter(tt.requeueAfter)
			err := opt(tt.initial)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, tt.initial.requeueAfter)
			}
		})
	}
}

func TestWithSyncWriter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sw      writer.SyncWriter
		initial *mcpServerReconcilerOptions
		wantErr bool
	}{
		{
			name:    "valid sync writer",
			sw:      &mockSyncWriter{},
			initial: &mcpServerReconcilerOptions{},
			wantErr: false,
		},
		{
			name:    "nil sync writer should error",
			sw:      nil,
			initial: &mcpServerReconcilerOptions{},
			wantErr: true,
		},
		{
			name:    "overwrite existing sync writer",
			sw:      &mockSyncWriter{},
			initial: &mcpServerReconcilerOptions{syncWriter: &mockSyncWriter{}},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opt := WithSyncWriter(tt.sw)
			err := opt(tt.initial)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, tt.initial.syncWriter)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, tt.initial.syncWriter)
				assert.Equal(t, tt.sw, tt.initial.syncWriter)
			}
		})
	}
}

func TestWithRegistryName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		regName string
		initial *mcpServerReconcilerOptions
		wantErr bool
	}{
		{
			name:    "valid registry name",
			regName: "test-registry",
			initial: &mcpServerReconcilerOptions{},
			wantErr: false,
		},
		{
			name:    "valid registry name with numbers",
			regName: "registry-123",
			initial: &mcpServerReconcilerOptions{},
			wantErr: false,
		},
		{
			name:    "valid registry name with underscores",
			regName: "test_registry_name",
			initial: &mcpServerReconcilerOptions{},
			wantErr: false,
		},
		{
			name:    "valid registry name with hyphens",
			regName: "my-registry-name",
			initial: &mcpServerReconcilerOptions{},
			wantErr: false,
		},
		{
			name:    "empty registry name should error",
			regName: "",
			initial: &mcpServerReconcilerOptions{},
			wantErr: true,
		},
		{
			name:    "valid long registry name",
			regName: "very-long-registry-name-with-many-parts",
			initial: &mcpServerReconcilerOptions{},
			wantErr: false,
		},
		{
			name:    "valid registry name with mixed case",
			regName: "MyRegistry-Name_123",
			initial: &mcpServerReconcilerOptions{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opt := WithRegistryName(tt.regName)
			err := opt(tt.initial)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.regName, tt.initial.registryName)
		})
	}
}

func TestWithLeaderElectionID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		id      string
		initial *mcpServerReconcilerOptions
		want    string
		wantErr bool
	}{
		{
			name:    "valid leader election ID",
			id:      "my-release-leader-election",
			initial: &mcpServerReconcilerOptions{},
			want:    "my-release-leader-election",
		},
		{
			name:    "empty ID should error",
			id:      "",
			initial: &mcpServerReconcilerOptions{},
			wantErr: true,
		},
		{
			name:    "overwrite existing ID",
			id:      "new-id",
			initial: &mcpServerReconcilerOptions{leaderElectionID: "old-id"},
			want:    "new-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opt := WithLeaderElectionID(tt.id)
			err := opt(tt.initial)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, tt.initial.leaderElectionID)
		})
	}
}

func TestHasRequiredRegistryAnnotations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		annotations map[string]string
		want        bool
	}{
		{
			name: "has both required annotations",
			annotations: map[string]string{
				defaultRegistryDescriptionAnnotation: "A test server",
				defaultRegistryURLAnnotation:         "https://example.com/mcp",
			},
			want: true,
		},
		{
			name: "has both required annotations with extra annotations",
			annotations: map[string]string{
				defaultRegistryDescriptionAnnotation: "A test server",
				defaultRegistryURLAnnotation:         "https://example.com/mcp",
				"some-other-annotation":              "value",
			},
			want: true,
		},
		{
			name: "missing description annotation",
			annotations: map[string]string{
				defaultRegistryURLAnnotation: "https://example.com/mcp",
			},
			want: false,
		},
		{
			name: "missing URL annotation",
			annotations: map[string]string{
				defaultRegistryDescriptionAnnotation: "A test server",
			},
			want: false,
		},
		{
			name:        "missing both annotations",
			annotations: map[string]string{},
			want:        false,
		},
		{
			name:        "nil annotations",
			annotations: nil,
			want:        false,
		},
		{
			name: "empty values still count as present",
			annotations: map[string]string{
				defaultRegistryDescriptionAnnotation: "",
				defaultRegistryURLAnnotation:         "",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := hasRequiredRegistryAnnotations(tt.annotations)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestReadNamespaceFromFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		wantNS  string
		wantErr bool
	}{
		{
			name:    "valid namespace",
			content: "my-namespace",
			wantNS:  "my-namespace",
			wantErr: false,
		},
		{
			name:    "namespace with trailing newline",
			content: "my-namespace\n",
			wantNS:  "my-namespace",
			wantErr: false,
		},
		{
			name:    "namespace with surrounding whitespace",
			content: "  my-namespace  \n",
			wantNS:  "my-namespace",
			wantErr: false,
		},
		{
			name:    "long namespace at max length",
			content: strings.Repeat("a", 253),
			wantNS:  strings.Repeat("a", 253),
			wantErr: false,
		},
		{
			name:    "content exceeding 256 bytes returns error",
			content: strings.Repeat("b", 300),
			wantNS:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create a temp file with the test content
			tmpDir := t.TempDir()
			tmpFile := filepath.Join(tmpDir, "namespace")
			err := os.WriteFile(tmpFile, []byte(tt.content), 0o600)
			require.NoError(t, err)

			got, err := readNamespaceFromFile(tmpFile)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantNS, got)
		})
	}

	t.Run("file does not exist", func(t *testing.T) {
		t.Parallel()

		_, err := readNamespaceFromFile("/nonexistent/path/namespace")

		require.Error(t, err)
	})
}

func TestValidateNamespaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		namespaces []string
		wantErr    bool
	}{
		{
			name:       "valid single namespace",
			namespaces: []string{"default"},
			wantErr:    false,
		},
		{
			name:       "valid multiple namespaces",
			namespaces: []string{"default", "kube-system", "my-app"},
			wantErr:    false,
		},
		{
			name:       "empty slice is valid",
			namespaces: []string{},
			wantErr:    false,
		},
		{
			name:       "nil slice is valid",
			namespaces: nil,
			wantErr:    false,
		},
		{
			name:       "single character namespace",
			namespaces: []string{"a"},
			wantErr:    false,
		},
		{
			name:       "namespace with numbers",
			namespaces: []string{"app123", "123app", "1a2b3c"},
			wantErr:    false,
		},
		{
			name:       "valid namespace at max length 63 chars",
			namespaces: []string{"a123456789012345678901234567890123456789012345678901234567890z"},
			wantErr:    false,
		},
		{
			name:       "namespace exceeding max length 64 chars",
			namespaces: []string{"a1234567890123456789012345678901234567890123456789012345678901234"},
			wantErr:    true,
		},
		{
			name:       "empty namespace name",
			namespaces: []string{""},
			wantErr:    true,
		},
		{
			name:       "namespace starting with hyphen",
			namespaces: []string{"-invalid"},
			wantErr:    true,
		},
		{
			name:       "namespace ending with hyphen",
			namespaces: []string{"invalid-"},
			wantErr:    true,
		},
		{
			name:       "namespace with uppercase letters",
			namespaces: []string{"Invalid"},
			wantErr:    true,
		},
		{
			name:       "namespace with underscores",
			namespaces: []string{"my_namespace"},
			wantErr:    true,
		},
		{
			name:       "namespace with special characters",
			namespaces: []string{"my.namespace"},
			wantErr:    true,
		},
		{
			name:       "namespace with spaces",
			namespaces: []string{"my namespace"},
			wantErr:    true,
		},
		{
			name:       "namespace with consecutive hyphens is valid",
			namespaces: []string{"my--namespace"},
			wantErr:    false,
		},
		{
			name:       "multiple namespaces with one invalid in middle",
			namespaces: []string{"valid", "Invalid", "also-valid"},
			wantErr:    true,
		},
		{
			name:       "multiple namespaces with first invalid",
			namespaces: []string{"INVALID", "valid", "also-valid"},
			wantErr:    true,
		},
		{
			name:       "multiple namespaces with last invalid",
			namespaces: []string{"valid", "also-valid", "invalid_one"},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateNamespaces(tt.namespaces)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
