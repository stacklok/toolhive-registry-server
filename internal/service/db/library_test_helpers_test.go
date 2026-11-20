package database

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

// withLibraryPublisher gives a test service the library adapter, with the
// claims hook, that performs managed publish and delete writes in production.
func withLibraryPublisher(t *testing.T, svc *dbService) {
	t.Helper()
	entries, err := postgres.NewEntries(context.Background(), svc.pool, svc.maxMetaSize,
		postgres.WithEntryWriteHook(NewClaimsHook(nil)))
	require.NoError(t, err)
	svc.publisher = entries
}

// latestManagedVersion reads the managed source's latest pointer for a name.
func latestManagedVersion(t *testing.T, svc *dbService, kind sqlc.EntryType, name string) (string, bool) {
	t.Helper()
	ctx := context.Background()
	q := sqlc.New(svc.pool)
	source, err := getManagedSource(ctx, q)
	require.NoError(t, err)
	version, err := q.GetLatestEntryVersion(ctx, sqlc.GetLatestEntryVersionParams{
		Name: name, EntryType: kind, SourceID: source.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false
	}
	require.NoError(t, err)
	return version, true
}

// slugName turns a test case name into a valid skill or plugin name
// (lower-case letters, digits and hyphens).
func slugName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
