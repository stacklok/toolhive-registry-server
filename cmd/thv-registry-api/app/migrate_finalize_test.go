package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
	registryapp "github.com/stacklok/toolhive-registry-server/internal/app"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	registrydb "github.com/stacklok/toolhive-registry-server/internal/db"
	"github.com/stacklok/toolhive-registry-server/internal/sync/writer"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
)

func TestOneFinalBinaryOfflineUpgrade(t *testing.T) {
	t.Parallel()
	testOneFinalBinaryOfflineUpgrade(t, true)
}

func TestDefaultPolicyOfflineUpgrade(t *testing.T) {
	t.Parallel()
	testOneFinalBinaryOfflineUpgrade(t, false)
}

func testOneFinalBinaryOfflineUpgrade(t *testing.T, custom bool) {
	t.Helper()
	conn, _ := database.SetupTestDBAt(t, 28)
	ctx := t.Context()
	dir := t.TempDir()
	policy := filepath.Join(dir, "policy.cedar")
	require.NoError(t, os.WriteFile(policy, []byte(`permit(principal, action == Registry::Action::"catalog.read", resource);`), 0600))
	policySetting := ""
	if custom {
		policySetting = " policyFile: " + policy + "\n"
	}
	path := filepath.Join(dir, "config.yaml")
	cc := conn.Config()
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(`database:
 host: %s
 port: %d
 user: %s
 password: %s
 database: %s
 sslMode: disable
auth:
 mode: anonymous
authorization:
%s allowAnonymousCatalog: true
sources:
 - name: future
   file:
     data: '{"version":"1.0.0","meta":{"last_updated":"2025-01-15T10:30:00Z"},"data":{"servers":[{"$schema":"https://example.org/server.schema.json","name":"com.example/inline","description":"future payload","version":"1"}]}}'
registries:
 - name: future
   sources: [future]
`, cc.Host, cc.Port, cc.User, cc.Password, cc.Database, policySetting)), 0600))
	_, err := conn.Exec(ctx, `INSERT INTO source(name,source_type,source_config,syncable,creation_type,claims)
 VALUES('history','managed','{}',false,'API','{"team":"private"}');
 INSERT INTO registry(name,creation_type,claims) VALUES('history','API','null');
 INSERT INTO registry_source(registry_id,source_id,position) SELECT r.id,s.id,0 FROM registry r,source s`)
	require.NoError(t, err)
	var snapshot model.Snapshot
	require.NoError(t, json.Unmarshal([]byte(`{"data":{
 "servers":[{"$schema":"https://example.org/server.json","name":"com.example/server","version":"1.0.0","description":"retained server"},
 {"$schema":"https://example.org/server.json","name":"com.example/server","version":"v2.0.0","description":"latest retained server"}],
 "skills":[{"namespace":"com.example","name":"skill","version":"1.0.0","description":"retained skill"},
 {"namespace":"com.example","name":"skill","version":"v2.0.0","description":"latest retained skill"}],
 "plugins":[{"namespace":"com.example","name":"plugin","version":"1.0.0","description":"retained plugin"},
 {"namespace":"com.example","name":"plugin","version":"v2.0.0","description":"latest retained plugin"}]}}`), &snapshot))
	var sourceID uuid.UUID
	require.NoError(t, conn.QueryRow(ctx, `SELECT id FROM source WHERE name='history'`).Scan(&sourceID))
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, writer.StoreSourceTx(ctx, tx, sourceID, &snapshot, 65536))
	require.NoError(t, tx.Commit(ctx))
	// Independent SQL snapshots retain payloads, opaque IDs and exact latest pointers.
	preserved := make(map[string]string)
	for _, table := range []string{"entry_version", "mcp_server", "skill", "plugin", "latest_entry_version"} {
		var rows string
		require.NoError(t, conn.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text)::text FROM `+table+` t`).Scan(&rows))
		preserved[table] = rows
	}
	migrationContext := ctx
	up := func(target uint) error {
		command := &cobra.Command{}
		command.SetContext(migrationContext)
		command.Flags().String("config", path, "")
		command.Flags().Bool("yes", true, "")
		command.Flags().Uint("to-version", target, "")
		if target != 0 {
			require.NoError(t, command.Flags().Set("to-version", fmt.Sprint(target)))
		}
		return runMigrateUp(command, nil)
	}
	var cancel context.CancelFunc
	migrationContext, cancel = context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, up(29), context.Canceled, "CLI must use the command context")
	migrationContext = ctx
	require.NoError(t, up(29))
	require.Error(t, up(28), "up must never downgrade")
	require.Error(t, up(0), "default up must refuse cleanly before consent")
	version, dirty, err := database.GetVersion(cc.ConnString())
	require.NoError(t, err)
	require.Equal(t, uint(29), version)
	require.False(t, dirty)
	exposure := func(args ...string) ([]byte, error) {
		command := newMigrateExposureCmd()
		command.PersistentFlags().String("config", path, "")
		command.PersistentFlags().Bool("yes", true, "")
		var output bytes.Buffer
		command.SetOut(&output)
		command.SetErr(&output)
		command.SetArgs(args)
		err := command.ExecuteContext(ctx)
		return output.Bytes(), err
	}
	_, err = exposure("finalize", "--yes")
	require.ErrorContains(t, err, "--confirm-offline")
	output, err := exposure("inventory")
	require.NoError(t, err)
	var inventory registrydb.ExposureInventory
	require.NoError(t, json.Unmarshal(output, &inventory))
	output, err = exposure("acknowledge", "--digest", inventory.Digest, "--approve-whole-catalog", "--confirm-offline")
	require.NoError(t, err)
	var ack registrydb.ExposureAcknowledgement
	require.NoError(t, json.Unmarshal(output, &ack))
	_, err = exposure("retire-claims", "--digest", ack.PostFenceDigest, "--acknowledgement-id", ack.ID, "--approve-whole-catalog", "--confirm-offline")
	require.NoError(t, err)
	require.Error(t, up(0), "retirement is not consumption")
	var sourcesBefore, sourcesAfter, latestBefore, latestAfter int
	require.NoError(t, conn.QueryRow(ctx, `SELECT count(*) FROM source`).Scan(&sourcesBefore))
	require.NoError(t, conn.QueryRow(ctx, `SELECT count(*) FROM latest_entry_version`).Scan(&latestBefore))
	_, err = exposure("finalize", "--confirm-offline")
	require.NoError(t, err)
	var receiptBefore, receiptAfter string
	require.NoError(t, conn.QueryRow(ctx, `SELECT row_to_json(e)::text FROM exposure_acknowledgement e`).Scan(&receiptBefore))
	_, err = exposure("finalize", "--confirm-offline")
	require.NoError(t, err, "consumed same-target verification is idempotent")
	require.NoError(t, conn.QueryRow(ctx, `SELECT row_to_json(e)::text FROM exposure_acknowledgement e`).Scan(&receiptAfter))
	require.Equal(t, receiptBefore, receiptAfter)
	require.NoError(t, conn.QueryRow(ctx, `SELECT count(*) FROM source`).Scan(&sourcesAfter))
	require.NoError(t, conn.QueryRow(ctx, `SELECT count(*) FROM latest_entry_version`).Scan(&latestAfter))
	require.Equal(t, sourcesBefore, sourcesAfter)
	require.Equal(t, latestBefore, latestAfter)
	require.NoError(t, up(0))
	for table, before := range preserved {
		var after string
		require.NoError(t, conn.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text)::text FROM `+table+` t`).Scan(&after))
		require.Equal(t, before, after, table+" survives finalization and schema cleanup verbatim")
	}
	cfg, err := config.LoadCatalogConfig(config.WithConfigPath(path))
	require.NoError(t, err)
	application, err := registryapp.NewCatalogApp(ctx, registryapp.CatalogOptions{Config: cfg,
		HTTP: []registryapp.RegistryAppOptions{registryapp.WithAddress("127.0.0.1:0"), registryapp.WithInternalAddress("127.0.0.1:0")}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = application.Stop() })
	require.NoError(t, application.Start())
	address, _ := application.Addresses()
	for _, route := range []struct{ path, description string }{
		{"/servers/com.example%2Fserver/versions/latest", "latest retained server"},
		{"/x/dev.toolhive/skills/com.example/skill", "latest retained skill"},
		{"/x/dev.toolhive/plugins/com.example/plugin", "latest retained plugin"},
	} {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/registry/history/v0.1"+route.path, nil)
		require.NoError(t, err)
		response, err := http.DefaultClient.Do(request)
		require.NoError(t, err)
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		require.Equal(t, http.StatusOK, response.StatusCode, string(body))
		require.Contains(t, string(body), route.description)
		require.Contains(t, string(body), `"version":"v2.0.0"`)
	}
	require.NoError(t, application.Stop())
}
