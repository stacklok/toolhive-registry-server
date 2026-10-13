package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

//nolint:paralleltest,tparallel // Exercises the real CLI's global Viper flag bindings.
func TestServeSelectsCatalogPreparationAndPropagatesListenerFailure(t *testing.T) {
	previousConfig, previousAddress, previousInternal := viper.Get("config"), viper.Get("address"), viper.Get("internal-address")
	t.Cleanup(func() {
		viper.Set("config", previousConfig)
		viper.Set("address", previousAddress)
		viper.Set("internal-address", previousInternal)
	})
	for _, state := range []string{"missing-receipt", "existing-null-claims", "fresh-terminal-listener"} {
		t.Run(state, func(t *testing.T) {
			conn, _ := database.SetupTestDB(t)
			cfg := writeServeCatalogConfig(t, conn.Config().Host, int(conn.Config().Port), conn.Config().User, conn.Config().Database, conn.Config().Password)
			if state == "existing-null-claims" {
				_, err := conn.Exec(t.Context(), `INSERT INTO source(name,source_type,source_config,syncable) VALUES('old','managed','{}',false)`)
				require.NoError(t, err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			defer listener.Close()
			viper.Set("config", cfg)
			viper.Set("address", listener.Addr().String())
			viper.Set("internal-address", "127.0.0.1:0")
			command := &cobra.Command{}
			command.SetContext(t.Context())
			command.Flags().String("auth-mode", "anonymous", "") // Final CLI override, not YAML.
			command.Flags().Bool("initialize-empty", state != "missing-receipt", "")
			err = runServe(command, nil)
			require.Error(t, err)
			var consumed bool
			var configured int
			require.NoError(t, conn.QueryRow(t.Context(), `SELECT consumed_at IS NOT NULL FROM exposure_acknowledgement`).Scan(&consumed))
			require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM source WHERE name='managed'`).Scan(&configured))
			if state == "fresh-terminal-listener" {
				require.True(t, consumed)
				require.Equal(t, 1, configured)
				require.ErrorContains(t, err, "address already in use")
			} else {
				require.False(t, consumed)
				require.Zero(t, configured, "no legacy automatic initialization before receipt barrier")
				require.ErrorContains(t, err, "docs/registry-exposure-migration.md")
			}
		})
	}
}

//nolint:paralleltest // Exercises the real CLI's global Viper flag bindings.
func TestServeEmptyBootstrapAndContextShutdown(t *testing.T) {
	previousConfig, previousAddress, previousInternal := viper.Get("config"), viper.Get("address"), viper.Get("internal-address")
	t.Cleanup(func() {
		viper.Set("config", previousConfig)
		viper.Set("address", previousAddress)
		viper.Set("internal-address", previousInternal)
	})
	conn, _ := database.SetupTestDB(t)
	cfg := writeServeCatalogConfig(t, conn.Config().Host, int(conn.Config().Port), conn.Config().User, conn.Config().Database, conn.Config().Password)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	viper.Set("config", cfg)
	viper.Set("address", address)
	viper.Set("internal-address", "127.0.0.1:0")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	command := &cobra.Command{}
	command.SetContext(ctx)
	command.Flags().String("auth-mode", "anonymous", "")
	command.Flags().Bool("initialize-empty", true, "")
	finished := make(chan error, 1)
	go func() { finished <- runServe(command, nil) }()
	client := &http.Client{Timeout: time.Second}
	require.Eventually(t, func() bool {
		response, err := client.Get("http://" + address + "/registry/public/v0.1/servers")
		if err != nil {
			return false
		}
		defer response.Body.Close()
		return response.StatusCode == http.StatusForbidden // Valid deny-all policy; no test-only exposure grant.
	}, 10*time.Second, 20*time.Millisecond)
	cancel()
	require.NoError(t, <-finished, "operator cancellation is normal; real terminal errors are returned separately")
	var mode string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT acknowledgement_mode FROM exposure_acknowledgement WHERE consumed_at IS NOT NULL`).Scan(&mode))
	require.Equal(t, "empty", mode)
}

//nolint:paralleltest // Exercises the real CLI's global Viper bindings and default lease timeout.
func TestServeInternalRenewDeadlineReturnsCommandFailure(t *testing.T) {
	previousConfig, previousAddress, previousInternal := viper.Get("config"), viper.Get("address"), viper.Get("internal-address")
	t.Cleanup(func() {
		viper.Set("config", previousConfig)
		viper.Set("address", previousAddress)
		viper.Set("internal-address", previousInternal)
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, canceled := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(canceled)
	}))
	defer func() { cancel(); upstream.Close() }()
	conn, _ := database.SetupTestDB(t)
	cfg := writeServeCatalogConfig(t, conn.Config().Host, int(conn.Config().Port), conn.Config().User, conn.Config().Database, conn.Config().Password)
	data, err := os.ReadFile(cfg)
	require.NoError(t, err)
	data = []byte(strings.Replace(string(data), "   managed: {}", "   file:\n    url: "+upstream.URL+"\n    timeout: 2m\n   syncPolicy:\n    interval: 1h", 1))
	require.NoError(t, os.WriteFile(cfg, data, 0600))
	viper.Set("config", cfg)
	viper.Set("address", "127.0.0.1:0")
	viper.Set("internal-address", "127.0.0.1:0")
	command := &cobra.Command{Use: "serve", RunE: runServe, SilenceUsage: true, SilenceErrors: true}
	command.SetContext(ctx)
	command.SetArgs(nil)
	command.Flags().String("auth-mode", "anonymous", "")
	command.Flags().Bool("initialize-empty", true, "")
	finished := make(chan error, 1)
	go func() { finished <- command.Execute() }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("leased fetch did not start")
	}
	// Hold the actual renewal's source lock until its child deadline expires.
	tx, err := conn.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	_, err = tx.Exec(t.Context(), `SELECT id FROM source WHERE name='managed' FOR UPDATE`)
	require.NoError(t, err)
	select {
	case <-canceled:
	case <-time.After(time.Minute):
		t.Fatal("renewal timeout did not cancel fetch")
	}
	require.NoError(t, tx.Rollback(t.Context())) // Allow fenced failure cleanup to finish.
	select {
	case err := <-finished:
		require.NoError(t, ctx.Err(), "the CLI parent lifetime is still active")
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.ErrorIs(t, err, persistence.ErrUnavailable, "Cobra must return an error for a nonzero process exit")
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not join the terminal runtime")
	}
}

func writeServeCatalogConfig(t *testing.T, host string, port int, user, databaseName, password string) string {
	t.Helper()
	dir := t.TempDir()
	policy := filepath.Join(dir, "policy.cedar")
	require.NoError(t, os.WriteFile(policy, []byte(`forbid(principal,action,resource);`), 0600))
	path := filepath.Join(dir, "config.yaml")
	data := fmt.Sprintf(`sources:
 - name: managed
   managed: {}
registries:
 - name: public
   sources: [managed]
database:
 host: %s
 port: %d
 user: %s
 database: %s
 password: %q
 sslMode: disable
auth:
 mode: oauth
admin:
 enabled: false
authorization:
 policyFile: %s
 allowAnonymousCatalog: true
`, host, port, user, databaseName, password, policy)
	require.NoError(t, os.WriteFile(path, []byte(data), 0600))
	return path
}
