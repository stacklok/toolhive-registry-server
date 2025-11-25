package database

import (
	"context"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, cleanupFunc := SetupTestDBContainer(t, ctx)
	t.Cleanup(cleanupFunc)

	connString := db.Config().ConnString()

	// Create migrate instance
	m, err := GetMigrate(connString)
	assert.NoError(t, err)
	defer m.Close()

	// Count the number of logical migrations
	fnames, err := fs.Glob(migrationsFS, "migrations/*.up.sql")
	require.NoError(t, err)

	// Test each migration individually: apply, rollback, re-apply
	for i := 1; i <= len(fnames); i++ {
		// Apply one migration
		err = m.Steps(1)
		assert.NoError(t, err, "Failed to apply migration %d", i)

		// Roll back one migration
		err = m.Steps(-1)
		assert.NoError(t, err, "Failed to roll back migration %d", i)

		// Re-apply the same migration
		err = m.Steps(1)
		assert.NoError(t, err, "Failed to re-apply migration %d", i)
	}

	// Test rolling back all migrations
	err = m.Down()
	assert.NoError(t, err, "Failed to roll back all migrations")

	// Test applying all migrations at once
	err = m.Up()
	assert.NoError(t, err, "Failed to apply all migrations")
}
