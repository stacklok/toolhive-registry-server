package app

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/app/storage"
	storagemocks "github.com/stacklok/toolhive-registry-server/internal/app/storage/mocks"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

func TestCatalogEmptyOptInRunsOnlyAfterPreflight(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"retired-config", "preflight", "empty-consume", "initialize"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			cfg := catalogTestConfig(t)
			ctrl := gomock.NewController(t)
			factory := storagemocks.NewMockCatalogFactory(ctrl)
			store := storagemocks.NewMockCatalogStorage(ctrl)
			fail := errors.New("stop preparation")
			if stage == "retired-config" {
				cfg.Auth.Authz = &config.AuthzConfig{}
			} else {
				factory.EXPECT().Open(gomock.Any(), gomock.Any()).Return(store, nil)
				store.EXPECT().Close()
				preflight := store.EXPECT().Preflight(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
				if stage == "preflight" {
					preflight.Return(nil, fail)
				} else {
					preflight.Return(nil, nil)
					consume := store.EXPECT().ConsumeEmpty(gomock.Any(), catalogRevision(t, cfg)).After(preflight)
					if stage == "empty-consume" {
						consume.Return(fail)
					} else {
						consume.Return(nil)
						store.EXPECT().Initialize(gomock.Any(), gomock.Any(), gomock.Any()).After(consume).Return(storage.CatalogComponents{}, fail)
					}
				}
			}
			app, err := NewCatalogApp(t.Context(), CatalogOptions{Config: cfg, InitializeEmpty: true, Storage: factory})
			require.Error(t, err)
			require.Nil(t, app)
		})
	}
}

func TestCatalogEmptyOptInFreezesPolicyBeforePreparation(t *testing.T) {
	t.Parallel()
	cfg := catalogTestConfig(t)
	ctrl := gomock.NewController(t)
	factory := storagemocks.NewMockCatalogFactory(ctrl)
	store := storagemocks.NewMockCatalogStorage(ctrl)
	// Open observes the final override, not the caller's unvalidated snapshot.
	factory.EXPECT().Open(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, final *config.Config) (storage.CatalogStorage, error) {
		require.Equal(t, "final", final.Registries[0].Name)
		return store, nil
	})
	final := cfg.Clone()
	final.Registries[0].Name = "final"
	store.EXPECT().Preflight(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
	store.EXPECT().ConsumeEmpty(gomock.Any(), catalogRevision(t, final)).Return(persistence.ErrUnavailable)
	store.EXPECT().Close()
	_, err := NewCatalogApp(t.Context(), CatalogOptions{Config: cfg, InitializeEmpty: true, Storage: factory,
		Overrides: []func(*config.Config) error{func(c *config.Config) error { c.Registries[0].Name = "final"; return nil }}})
	require.ErrorIs(t, err, persistence.ErrUnavailable)
	require.Equal(t, "public", cfg.Registries[0].Name)
}
