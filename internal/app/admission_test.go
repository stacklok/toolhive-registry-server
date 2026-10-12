package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/app/storage/mocks"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestAdmissionPreflightBeforeStorage(t *testing.T) {
	t.Parallel()
	for _, policy := range []string{"", "// comment only", "permit(", `permit(principal,action == Registry::Action::"unknown",resource);`} {
		t.Run(policy, func(t *testing.T) {
			t.Parallel()
			cfg := createValidTestConfig()
			cfg.Authorization.PolicyFile = filepath.Join(t.TempDir(), "policy.cedar")
			require.NoError(t, os.WriteFile(cfg.Authorization.PolicyFile, []byte(policy), 0600))
			// No expectations: any pool, state, writer or service construction fails the test.
			factory := mocks.NewMockFactory(gomock.NewController(t))
			app, err := NewRegistryApp(context.Background(), WithConfig(cfg), WithStorageFactory(factory))
			require.Error(t, err)
			require.Nil(t, app)
		})
	}
}

func TestAdmissionUnsafeConfigHasNoDownstreamConstruction(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*config.Config)
	}{
		{"injected public paths", func(c *config.Config) { c.Auth.PublicPaths = []string{"/"} }},
		{"anonymous legacy claims", func(c *config.Config) { c.Auth.Authz = &config.AuthzConfig{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := createValidTestConfig()
			tc.change(cfg)
			factory := mocks.NewMockFactory(gomock.NewController(t))
			app, err := NewRegistryApp(context.Background(), WithConfig(cfg), WithStorageFactory(factory))
			require.Error(t, err)
			require.Nil(t, app)
		})
	}
}
