package database

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// Admin-API source writes run the app's preparer before anything is stored:
// its changes (such as a defaulted Kubernetes namespace scope) are what is
// stored, and its error rejects the request as an invalid source config.
func TestSourceWritesRunThePreparer(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)
	var prepared []string
	unreachable := errors.New("kubernetes source \"k8s-src\": listing MCPServer in namespace team failed (Forbidden)")
	svc.prepare = func(_ context.Context, name string, req *service.SourceCreateRequest) error {
		prepared = append(prepared, name)
		if req.Kubernetes == nil {
			return nil
		}
		if name == "denied" {
			return unreachable
		}
		req.Kubernetes = &config.KubernetesConfig{Namespaces: []string{"team"}}
		return nil
	}
	ctx := t.Context()
	k8s := func() *service.SourceCreateRequest {
		return &service.SourceCreateRequest{Kubernetes: &config.KubernetesConfig{}}
	}

	info, err := svc.CreateSource(ctx, "k8s-src", k8s())
	require.NoError(t, err)
	require.Equal(t, &config.KubernetesConfig{Namespaces: []string{"team"}}, info.SourceConfig)
	stored, err := svc.GetSourceByName(ctx, "k8s-src")
	require.NoError(t, err)
	require.Equal(t, &config.KubernetesConfig{Namespaces: []string{"team"}}, stored.SourceConfig)

	_, err = svc.UpdateSource(ctx, "k8s-src", k8s())
	require.NoError(t, err)
	require.Equal(t, []string{"k8s-src", "k8s-src"}, prepared, "create and update both prepare")

	_, err = svc.CreateSource(ctx, "denied", k8s())
	require.ErrorIs(t, err, service.ErrInvalidSourceConfig)
	require.ErrorContains(t, err, "Forbidden")
	_, err = svc.GetSourceByName(ctx, "denied")
	require.ErrorIs(t, err, service.ErrSourceNotFound, "a rejected request stores nothing")
}
