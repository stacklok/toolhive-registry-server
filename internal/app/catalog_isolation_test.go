package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	mcp "github.com/stacklok/toolhive/cmd/thv-operator/api/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/kubernetes"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// One source's broken definition must not stop sync for the others. A stored
// Kubernetes definition the operator rejects (here, an invalid namespace that
// an older server or a direct database edit left behind) is marked failed and
// retried, while a file source and an inline source complete.
func TestSync_BrokenKubernetesSourceDoesNotStopOtherSources(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "registry.json")
	require.NoError(t, os.WriteFile(path, []byte(validInlineRegistry), 0o600))
	cfg := catalogTestConfig(newTestDatabaseConfig(t))
	cfg.Sources = append(cfg.Sources,
		config.SourceConfig{Name: "file-src", File: &config.FileConfig{Path: path}, SyncPolicy: &config.SyncPolicyConfig{Interval: "1h"}},
		config.SourceConfig{Name: "k8s-src", Kubernetes: &config.KubernetesConfig{Namespaces: []string{"default"}}},
	)
	scheme := runtime.NewScheme()
	require.NoError(t, mcp.AddToScheme(scheme))
	b := newBuildCatalogConfig(t, cfg)
	b.operator = &kubernetes.StandaloneFactory{
		API: fake.NewClientBuilder().WithScheme(scheme).Build(), Claims: kubernetes.NewAnnotationClaims(),
	}
	built, err := buildCatalog(ctx, b)
	require.NoError(t, err)
	_, err = built.catalog.Pool.Exec(ctx,
		`UPDATE source SET source_config = '{"namespaces":["Bad_NS"]}' WHERE name = 'k8s-src'`)
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() { done <- built.sync.Start(ctx) }()
	t.Cleanup(func() {
		require.NoError(t, built.sync.Stop())
		require.NoError(t, <-done)
	})

	phase := func(name string) persistence.JobPhase {
		status, err := built.catalog.Jobs.Status(ctx, name)
		if err != nil {
			return ""
		}
		return status.Phase
	}
	require.Eventually(t, func() bool {
		return phase("file-src") == persistence.JobCompleted && phase("inline") == persistence.JobCompleted &&
			phase("k8s-src") == persistence.JobFailed
	}, 20*time.Second, 50*time.Millisecond, "file and inline sources sync; the kubernetes source is marked failed")
	status, err := built.catalog.Jobs.Status(ctx, "k8s-src")
	require.NoError(t, err)
	assert.Equal(t, persistence.FailureValidation, status.Failure)
	require.Eventually(t, built.sync.Ready, 5*time.Second, 10*time.Millisecond, "the runtime keeps running")
}
