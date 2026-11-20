package database

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/auth"
)

func TestSourceWritesNotifySyncRuntime(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestServiceWithCodecs(t)
	t.Cleanup(cleanup)
	var calls atomic.Int32
	svc.sourcesChanged = func(context.Context) error {
		calls.Add(1)
		return errors.New("runtime not running") // failures are logged, never returned
	}
	ctx := t.Context()

	_, err := svc.CreateSource(ctx, "inline-src", fileDataSourceReq(nil))
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load(), "create notifies")

	_, err = svc.UpdateSource(ctx, "inline-src", fileDataSourceReq(nil))
	require.NoError(t, err)
	require.EqualValues(t, 2, calls.Load(), "update notifies")

	require.NoError(t, svc.DeleteSource(ctx, "inline-src"))
	require.EqualValues(t, 3, calls.Load(), "delete notifies")

	require.Error(t, svc.DeleteSource(ctx, "inline-src"))
	denied := auth.ContextWithClaims(ctx, jwt.MapClaims{"org": "acme"})
	_, err = svc.CreateSource(denied, "denied-src", fileDataSourceReq(map[string]any{"org": "other"}))
	require.Error(t, err)
	require.EqualValues(t, 3, calls.Load(), "failed writes do not notify")
}
