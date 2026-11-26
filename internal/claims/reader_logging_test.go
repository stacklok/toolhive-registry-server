package claims_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/claims"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

type failingRegistries struct{ err error }

func (f failingRegistries) GetRegistryByName(context.Context, string) (sqlc.Registry, error) {
	return sqlc.Registry{}, f.err
}

// unreachableReader fails the test if the claims reader reads past its gate.
type unreachableReader struct {
	persistence.ViewEntryReader
	t *testing.T
}

func (u unreachableReader) ListViewEntries(context.Context, string, persistence.ViewQuery) (persistence.EntryPage, error) {
	u.t.Error("the view must not be read when the registry lookup fails")
	return persistence.EntryPage{}, nil
}

// TestReaderLogsRegistryLookupFailure covers part of F8: the API answers a
// failed registry lookup with a sanitized 500, so the cause is logged here.
//
//nolint:paralleltest // replaces the process-wide default slog logger
func TestReaderLogsRegistryLookupFailure(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	reader, err := claims.NewReader(unreachableReader{t: t}, failingRegistries{err: errors.New("connection refused")})
	require.NoError(t, err)
	_, err = reader.ListViewEntries(caller(jwt.MapClaims{"sub": "c", "org": "acme"}), "main",
		persistence.ViewQuery{Kind: persistence.SkillKind, Limit: 10})
	require.ErrorIs(t, err, persistence.ErrUnavailable)

	var rec map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &rec), buf.String())
	assert.Equal(t, "ERROR", rec["level"])
	assert.Equal(t, "Failed to look up registry claims", rec["msg"])
	assert.Equal(t, "main", rec["registry"])
	assert.Equal(t, "connection refused", rec["error"])
}
