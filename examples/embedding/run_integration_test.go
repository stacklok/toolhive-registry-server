//go:build integration

package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

func TestRunWithPostgresAndMutualTLS(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDB(t) // Disposable embedded PostgreSQL, clean schema 30; never an environment DSN.
	pool, err := pgxpool.New(t.Context(), conn.Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	defs, err := postgres.NewDefinitions(pool)
	require.NoError(t, err)
	source, err := defs.CreateSource(t.Context(), persistence.SourceDefinition{
		Name: "tutorial", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h",
	})
	require.NoError(t, err)
	_, err = defs.CreateView(t.Context(), persistence.ViewDefinition{Name: "default", Sources: []string{source.Name}})
	require.NoError(t, err)
	entries, err := postgres.NewEntries(t.Context(), pool, 65536)
	require.NoError(t, err)
	snapshot := model.Snapshot{}
	snapshot.Data.Servers = []model.Server{{
		Schema: "https://example.org/server.schema.json", Name: "com.example/tutorial", Version: "1.0.0", Description: "tutorial fixture",
	}}
	require.NoError(t, entries.ReplaceSnapshot(t.Context(), source, snapshot))

	fixtures := newTestCertificates(t)
	dsnFile := filepath.Join(t.TempDir(), "dsn")
	require.NoError(t, os.WriteFile(dsnFile, []byte(conn.Config().ConnString()), 0600))
	// Reserve an ephemeral port, then release it for the unchanged run entry point.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	_, port, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	require.NoError(t, listener.Close())
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- run(ctx, addr, dsnFile, fixtures.certFile, fixtures.keyFile, fixtures.caFile) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(15 * time.Second):
			t.Error("application did not shut down and join")
		}
	})
	client := fixtures.httpClient(t, &fixtures.client)
	url := "https://" + net.JoinHostPort("localhost", port) + "/registry/default/v0.1/servers"
	var response *http.Response
	require.Eventually(t, func() bool {
		response, err = client.Get(url)
		return err == nil
	}, 10*time.Second, 20*time.Millisecond, "application did not become reachable")
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	// Independent wire expectation; no production DTO or serializer as oracle.
	var page struct {
		Servers []struct {
			Server struct {
				Name, Version, Description string
			}
		}
		Metadata struct{ Count int }
	}
	require.NoError(t, json.Unmarshal(body, &page))
	require.Equal(t, 1, page.Metadata.Count)
	require.Len(t, page.Servers, 1)
	require.Equal(t, "com.example/tutorial", page.Servers[0].Server.Name)
	require.Equal(t, "1.0.0", page.Servers[0].Server.Version)
	require.Equal(t, "tutorial fixture", page.Servers[0].Server.Description)
	noCert := fixtures.httpClient(t, nil)
	resp, err := noCert.Get(url)
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err, "no certificate must be rejected by TLS, not HTTP")
	cancel()
	// Cleanup verifies run returns successfully after cancellation, before DB teardown.
}
