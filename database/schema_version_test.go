package database_test

import (
	"io/fs"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
)

// LatestSchemaVersion is hand-maintained; a new migration must bump it, or
// test databases and the library's schema check stop one version short.
func TestLatestSchemaVersionMatchesNewestMigration(t *testing.T) {
	t.Parallel()

	entries, err := fs.ReadDir(database.MigrationsFS, "migrations")
	require.NoError(t, err)
	var newest uint64
	for _, e := range entries {
		prefix, _, ok := strings.Cut(e.Name(), "_")
		require.True(t, ok, "unexpected migration file name %q", e.Name())
		n, err := strconv.ParseUint(prefix, 10, 32)
		require.NoError(t, err, e.Name())
		newest = max(newest, n)
	}
	require.Equal(t, uint(newest), database.LatestSchemaVersion)
}

// Library hosts import this package to run migrations, so it must not link
// test-only dependencies into their binaries; those live in database/dbtest.
func TestPackageHasNoTestDependencies(t *testing.T) {
	t.Parallel()

	out, err := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", ".").CombinedOutput()
	require.NoError(t, err, string(out))
	for _, dep := range strings.Fields(string(out)) {
		require.NotEqual(t, "testing", dep)
		require.NotContains(t, dep, "github.com/stretchr/testify")
		require.NotContains(t, dep, "embedded-postgres")
	}
}
