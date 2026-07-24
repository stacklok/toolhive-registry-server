package storage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestNewStorageFactory_MeterProviderWiring(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, cleanupFunc := database.SetupTestDBContainer(t, ctx)
	t.Cleanup(cleanupFunc)
	require.NoError(t, database.MigrateUp(ctx, db))

	cfg := &config.Config{
		Database: &config.DatabaseConfig{
			Host:     db.Config().Host,
			Port:     int(db.Config().Port),
			User:     db.Config().User,
			Database: db.Config().Database,
			SSLMode:  "disable",
		},
	}

	t.Run("meterProvider set builds DBMetrics and wires it into the factory", func(t *testing.T) {
		t.Parallel()

		factory, err := NewStorageFactory(ctx, cfg, WithMeterProvider(sdkmetric.NewMeterProvider()))
		require.NoError(t, err)
		require.NotNil(t, factory)

		dbFactory, ok := factory.(*DatabaseFactory)
		require.True(t, ok)
		t.Cleanup(dbFactory.Cleanup)

		assert.NotNil(t, dbFactory.dbMetrics, "WithMeterProvider should result in a non-nil DBMetrics on the factory")
	})

	t.Run("no meterProvider leaves DBMetrics unset", func(t *testing.T) {
		t.Parallel()

		factory, err := NewStorageFactory(ctx, cfg)
		require.NoError(t, err)
		require.NotNil(t, factory)

		dbFactory, ok := factory.(*DatabaseFactory)
		require.True(t, ok)
		t.Cleanup(dbFactory.Cleanup)

		assert.Nil(t, dbFactory.dbMetrics, "no WithMeterProvider option should leave dbMetrics nil")
	})

	t.Run("noop meterProvider still builds DBMetrics (only a nil provider short-circuits)", func(t *testing.T) {
		t.Parallel()

		factory, err := NewStorageFactory(ctx, cfg, WithMeterProvider(noop.NewMeterProvider()))
		require.NoError(t, err)
		require.NotNil(t, factory)

		dbFactory, ok := factory.(*DatabaseFactory)
		require.True(t, ok)
		t.Cleanup(dbFactory.Cleanup)

		assert.NotNil(t, dbFactory.dbMetrics)
	})
}
