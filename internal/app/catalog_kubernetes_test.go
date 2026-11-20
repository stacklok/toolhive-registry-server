package app

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/kubernetes"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// Admin-API Kubernetes sources get the same scope and preflight as config
// sources in claims mode, as they do in Cedar mode.
func TestKubernetesSourcePreparer(t *testing.T) {
	t.Parallel()
	refused := errors.New("forbidden")
	for _, tc := range []struct {
		name          string
		watch         string
		namespaces    []string
		preflightErr  error
		wantScope     []string
		wantErr       error
		wantPreflight bool
	}{
		{name: "no namespaces uses the watch namespaces", watch: "team-b, team-a", wantScope: []string{"team-a", "team-b"},
			wantPreflight: true},
		{name: "no namespaces and no watch namespace means all", wantScope: []string{}, wantPreflight: true},
		{name: "explicit namespaces win and are canonical", watch: "other", namespaces: []string{"b", "a", "b"},
			wantScope: []string{"a", "b"}, wantPreflight: true},
		{name: "invalid namespace", namespaces: []string{"Bad_NS"}, wantErr: persistence.ErrInvalid},
		{name: "preflight failure rejects the request", namespaces: []string{"a"}, preflightErr: refused,
			wantErr: refused, wantPreflight: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var checked *persistence.SourceDefinition
			prepare := kubernetesSourcePreparer(tc.watch, func(_ context.Context, d persistence.SourceDefinition) error {
				checked = &d
				return tc.preflightErr
			})
			req := &service.SourceCreateRequest{Kubernetes: &config.KubernetesConfig{Namespaces: tc.namespaces}}
			err := prepare(t.Context(), "k8s-src", req)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
				assert.ElementsMatch(t, tc.wantScope, req.Kubernetes.Namespaces, "the effective scope is stored")
			}
			require.Equal(t, tc.wantPreflight, checked != nil)
			if checked != nil {
				assert.Equal(t, "k8s-src", checked.Name)
				assert.ElementsMatch(t, req.Kubernetes.Namespaces, checked.Kubernetes.Namespaces)
			}
		})
	}

	// Other source types pass through untouched.
	prepare := kubernetesSourcePreparer("team", func(context.Context, persistence.SourceDefinition) error {
		return errors.New("preflight must not run")
	})
	require.NoError(t, prepare(t.Context(), "file-src", &service.SourceCreateRequest{File: &config.FileConfig{Path: "x"}}))
}

func TestStartupPreflight(t *testing.T) {
	t.Parallel()
	k8s := persistence.SourceDefinition{Name: "k8s-src", Kubernetes: &persistence.KubernetesSpec{Namespaces: []string{"a"}}}
	file := persistence.SourceDefinition{Name: "file-src", File: &persistence.FileSpec{Path: "x"}}
	listFailed := &kubernetes.PreflightError{Source: "k8s-src", Kind: "MCPServer", Namespace: "a", Err: errors.New("connection refused")}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	for _, tc := range []struct {
		name    string
		ctx     context.Context
		d       persistence.SourceDefinition
		err     error
		wantErr bool
	}{
		{name: "ok", ctx: t.Context(), d: k8s},
		{name: "kubernetes cannot be listed: logged, startup continues", ctx: t.Context(), d: k8s, err: listFailed},
		{name: "kubernetes client unavailable: logged, startup continues", ctx: t.Context(), d: k8s,
			err: errors.New("kubernetes client configuration unavailable")},
		{name: "invalid kubernetes definition stops startup", ctx: t.Context(), d: k8s, err: persistence.ErrInvalid, wantErr: true},
		{name: "other source types stop startup", ctx: t.Context(), d: file, err: errors.New("bad filter"), wantErr: true},
		{name: "cancellation stops startup", ctx: cancelled, d: k8s, err: context.Canceled, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := startupPreflight(tc.ctx, tc.d, func(context.Context, persistence.SourceDefinition) error { return tc.err })
			if tc.wantErr {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
