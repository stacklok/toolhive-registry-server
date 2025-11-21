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

	for i := 1; i <= len(fnames); i++ {
		// step up
		err = m.Steps(i)
		assert.NoError(t, err)

		// step down
		err = m.Steps(-i)
		assert.NoError(t, err)

		// step up again
		err = m.Steps(i)
		assert.NoError(t, err)
	}
}
