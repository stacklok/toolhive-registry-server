package app

import (
	"bytes"
	"encoding/json"
	"errors"
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

type failingOutputWriter struct{}

func (failingOutputWriter) Write([]byte) (int, error) {
	return 0, errors.New("output unavailable")
}

//nolint:paralleltest // Capture the process-global logger to verify no secrets are logged.
func TestMigrateExposureCLI(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	conn, _ := database.SetupTestDBAt(t, 29)
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
	var pending registrydb.ExposureAcknowledgement
	require.NoError(t, json.Unmarshal([]byte(ackOutput), &pending))
	for _, args := range [][]string{
		{"retire-claims", "--digest", pending.PostFenceDigest, "--acknowledgement-id", pending.ID, "--yes"},
		{"retire-claims", "--digest", pending.PostFenceDigest, "--acknowledgement-id", pending.ID, "--confirm-offline"},
		{"retire-claims", "--digest", pending.PostFenceDigest, "--acknowledgement-id", pending.ID, "--approve-whole-catalog"},
	} {
		_, err = execute(args...)
		require.ErrorContains(t, err, "explicit exposure consent")
	}
	_, err = execute("retire-claims", "--digest", pending.PostFenceDigest, "--approve-whole-catalog", "--confirm-offline")
	require.ErrorContains(t, err, "acknowledgement-id")
	_, err = execute("retire-claims", "--digest", pending.PostFenceDigest, "--acknowledgement-id", pending.ID, "--approve-whole-catalog", "--confirm-offline")
	require.ErrorContains(t, err, "requires final configuration without retired fields")
	// Changing to final config invalidates the old acknowledgement, not a silent rebind.
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	data = bytes.ReplaceAll(data, []byte(" publicPaths: null\n authz: null\n"), []byte(" mode: oauth\n"))
	require.NoError(t, os.WriteFile(path, data, 0600))
	_, err = execute("retire-claims", "--digest", pending.PostFenceDigest, "--acknowledgement-id", pending.ID, "--approve-whole-catalog", "--confirm-offline")
	require.ErrorContains(t, err, "acknowledgement mismatch")
	var retainedClaims string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT claims::text FROM source WHERE name='persisted'`).Scan(&retainedClaims))
	require.Contains(t, retainedClaims, "private-claims-sentinel")
	output, err = execute("inventory")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(output), &inventory))
	ackOutput, err = execute("acknowledge", "--digest", inventory.Digest, "--approve-whole-catalog", "--confirm-offline")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(ackOutput), &pending))
	retireOutput, err := execute("retire-claims", "--digest", pending.PostFenceDigest, "--acknowledgement-id", pending.ID, "--approve-whole-catalog", "--confirm-offline")
	require.NoError(t, err)
	require.Contains(t, retireOutput, "metadata retired; runtime cutover not yet available in this slice")
	require.NotContains(t, retireOutput, "cutover complete")
	var receipt registrydb.ExposureRetirement
	require.NoError(t, json.Unmarshal([]byte(retireOutput), &receipt))
	require.Equal(t, pending.ReviewedDigest, receipt.ReviewedDigest)
	_, err = execute("retire-claims", "--digest", pending.PostFenceDigest, "--acknowledgement-id", pending.ID, "--approve-whole-catalog", "--confirm-offline")
	require.ErrorContains(t, err, "already transitioned")
	for _, secret := range []string{"private-policy-sentinel", "private-payload-sentinel", "private-claims-sentinel", database.DBPass} {
		require.NotContains(t, output+ackOutput+retireOutput+logs.String(), secret)
	}
}

func TestMigrateExposureRetirementOutputFailureKeepsOffline(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDBAt(t, 29)
	path := exposureCLIConfig(t, conn)
	_, err := conn.Exec(t.Context(), `INSERT INTO source(name,source_type,source_config,claims)
VALUES('persisted','file','{}','{"team":"private-claims-sentinel"}')`)
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	data = bytes.ReplaceAll(data, []byte(" publicPaths: null\n authz: null\n"), []byte(" mode: oauth\n"))
	require.NoError(t, os.WriteFile(path, data, 0600))
	execute := func(writer *bytes.Buffer, args ...string) error {
		t.Helper()
		parent := &cobra.Command{Use: "migrate", SilenceErrors: true, SilenceUsage: true}
		parent.PersistentFlags().String("config", path, "")
		parent.AddCommand(newMigrateExposureCmd())
		parent.SetOut(writer)
		parent.SetErr(writer)
		parent.SetArgs(append([]string{"exposure"}, args...))
		return parent.ExecuteContext(t.Context())
	}
	var output bytes.Buffer
	require.NoError(t, execute(&output, "inventory"))
	var inventory registrydb.ExposureInventory
	require.NoError(t, json.Unmarshal(output.Bytes(), &inventory))
	output.Reset()
	require.NoError(t, execute(&output, "acknowledge", "--digest", inventory.Digest,
		"--approve-whole-catalog", "--confirm-offline"))
	var acknowledgement registrydb.ExposureAcknowledgement
	require.NoError(t, json.Unmarshal(output.Bytes(), &acknowledgement))

	parent := &cobra.Command{Use: "migrate", SilenceErrors: true, SilenceUsage: true}
	parent.PersistentFlags().String("config", path, "")
	parent.AddCommand(newMigrateExposureCmd())
	parent.SetOut(failingOutputWriter{})
	parent.SetErr(failingOutputWriter{})
	parent.SetArgs([]string{"exposure", "retire-claims", "--digest", acknowledgement.PostFenceDigest,
		"--acknowledgement-id", acknowledgement.ID, "--approve-whole-catalog", "--confirm-offline"})
	err = parent.ExecuteContext(t.Context())
	require.EqualError(t, err,
		"claim metadata retirement committed; remain offline; inspect stored receipt in docs/registry-exposure-migration.md")
	var retirementID string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT retirement_id::text FROM exposure_acknowledgement WHERE singleton`).Scan(&retirementID))
	require.NotEmpty(t, retirementID)
	var claims *string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT claims::text FROM source WHERE name='persisted'`).Scan(&claims))
	require.Nil(t, claims)
}

func TestMigrateExposureInventoryDoesNotAutoMigrate(t *testing.T) {
	t.Parallel()
	conn, _ := database.SetupTestDBAt(t, 29)
	require.NoError(t, database.MigrateDown(t.Context(), conn, 3))
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
