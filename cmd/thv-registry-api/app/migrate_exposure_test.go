package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
	registrydb "github.com/stacklok/toolhive-registry-server/internal/db"
)

//nolint:paralleltest // Capture the process-global logger to verify no secrets are logged.
func TestMigrateExposureCLI(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	conn, _ := database.SetupTestDB(t)
	path := exposureCLIConfig(t, conn)
	execute := func(args ...string) (string, error) {
		t.Helper()
		parent := &cobra.Command{Use: "migrate", SilenceErrors: true, SilenceUsage: true}
		parent.PersistentFlags().String("config", path, "")
		parent.PersistentFlags().Bool("yes", false, "")
		parent.AddCommand(newMigrateExposureCmd())
		var output bytes.Buffer
		parent.SetOut(&output)
		parent.SetErr(&output)
		parent.SetArgs(append([]string{"exposure"}, args...))
		err := parent.ExecuteContext(t.Context())
		return output.String(), err
	}
	output, err := execute("inventory")
	require.NoError(t, err)
	var inventory registrydb.ExposureInventory
	require.NoError(t, json.Unmarshal([]byte(output), &inventory))
	require.NotEmpty(t, inventory.Digest)
	require.Contains(t, output, "legacy protections unchanged")
	require.Contains(t, output, "auth.publicpaths")
	for _, args := range [][]string{
		{"acknowledge", "--yes", "--digest", inventory.Digest},
		{"acknowledge", "--digest", inventory.Digest, "--confirm-offline"},
		{"acknowledge", "--digest", inventory.Digest, "--approve-whole-catalog"},
		{"initialize-empty", "--yes", "--digest", inventory.Digest},
	} {
		_, err := execute(args...)
		require.ErrorContains(t, err, "explicit exposure consent")
	}
	var marker *string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT acknowledgement_id::text FROM exposure_acknowledgement`).Scan(&marker))
	require.Nil(t, marker)
	output, err = execute("initialize-empty", "--digest", inventory.Digest, "--confirm-offline")
	require.NoError(t, err, "future YAML sources do not make an unreconciled database nonempty")
	require.Contains(t, output, "acknowledgement recorded; legacy protections unchanged")
	require.NotContains(t, output, "cutover complete")
	var ack registrydb.ExposureAcknowledgement
	require.NoError(t, json.Unmarshal([]byte(output), &ack))
	require.Equal(t, "empty", ack.Mode)
	_, err = conn.Exec(t.Context(), `INSERT INTO source(name,source_type,source_config,claims)
 VALUES('persisted','file','{"data":"private-payload-sentinel"}','{"team":"private-claims-sentinel"}')`)
	require.NoError(t, err)
	output, err = execute("inventory")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(output), &inventory))
	_, err = execute("initialize-empty", "--digest", inventory.Digest, "--confirm-offline")
	require.ErrorContains(t, err, "every persisted catalog table")
	_, err = execute("acknowledge", "--digest", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--approve-whole-catalog", "--confirm-offline")
	require.ErrorContains(t, err, "digest mismatch")
	ackOutput, err := execute("acknowledge", "--digest", inventory.Digest, "--approve-whole-catalog", "--confirm-offline")
	require.NoError(t, err)
	for _, secret := range []string{"private-policy-sentinel", "private-payload-sentinel", "private-claims-sentinel", database.DBPass} {
		require.NotContains(t, output+ackOutput+logs.String(), secret)
	}
}

func TestMigrateExposureInventoryDoesNotAutoMigrate(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDB(t)
	require.NoError(t, database.MigrateDown(t.Context(), conn, 1))
	command := newMigrateExposureCmd()
	command.PersistentFlags().String("config", exposureCLIConfig(t, conn), "")
	command.SetArgs([]string{"inventory"})
	require.ErrorContains(t, command.ExecuteContext(t.Context()), "requires clean schema version 27")
	version, dirty, err := database.GetVersion(conn.Config().ConnString())
	require.NoError(t, err)
	require.Equal(t, uint(26), version)
	require.False(t, dirty)
	var count int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM source`).Scan(&count))
	require.Zero(t, count)
}

func exposureCLIConfig(t *testing.T, conn *pgx.Conn) string {
	t.Helper()
	policy := filepath.Join(t.TempDir(), "policy.cedar")
	require.NoError(t, os.WriteFile(policy, []byte("private-policy-sentinel"), 0600))
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := conn.Config()
	data := fmt.Sprintf(`database:
 host: %s
 port: %d
 user: %s
 password: %s
 database: %s
 sslMode: disable
sources:
 - name: future
   api: {endpoint: 'https://must-not-fetch.invalid'}
auth:
 publicPaths: null
 authz: null
authorization:
 policyFile: %s
`, cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Database, policy)
	require.NoError(t, os.WriteFile(path, []byte(data), 0600))
	return path
}
