package app

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	storagemocks "github.com/stacklok/toolhive-registry-server/internal/app/storage/mocks"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

func TestFinalizeStopsBeforeInitialization(t *testing.T) {
	t.Parallel()
	for _, failPreflight := range []bool{false, true} {
		t.Run(map[bool]string{false: "consume", true: "preflight-refusal"}[failPreflight], func(t *testing.T) {
			t.Parallel()
			cfg := catalogTestConfig(t)
			revision := catalogRevision(t, cfg)
			ctrl := gomock.NewController(t)
			factory := storagemocks.NewMockCatalogFactory(ctrl)
			store := storagemocks.NewMockCatalogStorage(ctrl)
			factory.EXPECT().Open(gomock.Any(), gomock.Any()).Return(store, nil)
			preflight := store.EXPECT().Preflight(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
			if failPreflight {
				preflight.Return(nil, errors.New("historical incompatibility"))
			} else {
				// Editing policy after load cannot change the revision consumed by
				// the shared barrier. No Initialize/ConsumeEmpty/worker call is allowed.
				preflight.Do(func(context.Context, []persistence.SourceDefinition, []persistence.ViewDefinition, string) {
					require.NoError(t, os.WriteFile(cfg.Authorization.PolicyFile, []byte(`forbid(principal,action,resource);`), 0600))
				}).Return(nil, nil)
				store.EXPECT().Consume(gomock.Any(), revision).After(preflight).Return(nil)
			}
			store.EXPECT().Close()
			err := FinalizeExposure(t.Context(), CatalogOptions{Config: cfg, Storage: factory})
			if failPreflight {
				require.ErrorContains(t, err, "historical incompatibility")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
