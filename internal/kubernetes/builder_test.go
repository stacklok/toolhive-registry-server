package kubernetes

import (
	"context"
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
		want       []string
		wantErr    bool
	}{
		{
			name:       "single namespace",
			namespaces: []string{"default"},
			initial:    &mcpServerReconcilerOptions{},
			want:       []string{"default"},
			wantErr:    false,
		},
		{
			name:       "multiple namespaces",
			namespaces: []string{"default", "kube-system", "production"},
			initial:    &mcpServerReconcilerOptions{},
			want:       []string{"default", "kube-system", "production"},
			wantErr:    false,
		},
		{
			name:       "empty namespaces",
			namespaces: []string{},
			initial:    &mcpServerReconcilerOptions{},
			want:       []string{},
			wantErr:    false,
		},
		{
			name:       "nil initial namespaces",
			namespaces: []string{"default"},
			initial:    &mcpServerReconcilerOptions{namespaces: nil},
			want:       []string{"default"},
			wantErr:    false,
		},
		{
			name:       "append to existing namespaces",
			namespaces: []string{"new-namespace"},
			initial:    &mcpServerReconcilerOptions{namespaces: []string{"existing"}},
			want:       []string{"existing", "new-namespace"},
			wantErr:    false,
		},
		{
			name:       "multiple appends",
			namespaces: []string{"ns1", "ns2", "ns3"},
			initial:    &mcpServerReconcilerOptions{namespaces: []string{"existing"}},
			want:       []string{"existing", "ns1", "ns2", "ns3"},
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opt := WithNamespaces(tt.namespaces...)
			err := opt(tt.initial)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, tt.initial.namespaces)
			}
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
