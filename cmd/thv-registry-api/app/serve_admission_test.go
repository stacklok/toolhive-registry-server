package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

//nolint:paralleltest // runServe consumes the CLI's global Viper configuration.
func TestServeCancellationBeforeConstructionIsNormal(t *testing.T) {
	previous := viper.Get("config")
	t.Cleanup(func() { viper.Set("config", previous) })
	viper.Set("config", writeServeCatalogConfig(t, "localhost", 1, "registry", "registry", "fixture"))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	command := &cobra.Command{}
	command.SetContext(ctx)
	command.Flags().String("auth-mode", "anonymous", "")
	command.Flags().Bool("initialize-empty", true, "")
	require.NoError(t, runServe(command, nil))
	require.Equal(t, "false", serveCmd.Flags().Lookup("initialize-empty").DefValue)
	require.Nil(t, serveCmd.Flags().Lookup("yes"), "migration --yes must never become startup consent")
}

//nolint:paralleltest,tparallel // runServe consumes the CLI's global Viper configuration.
func TestServeAdmissionPreflightAfterOverride(t *testing.T) {
	previous := viper.Get("config")
	t.Cleanup(func() { viper.Set("config", previous) })
	for _, tc := range []struct{ name, override, want string }{
		{"omitted auth defaults to OAuth", "", "auth.oauth is required"},
		{"omitted auth CLI override", "anonymous", "admin.enabled requires OAuth"},
		{"invalid CLI mode", "invalid", "invalid auth.mode"},
		{"anonymous CLI override with admin", "anonymous", "admin.enabled requires OAuth"},
		{"retired leader election override", "", "THV_REGISTRY_LEADER_ELECTION_ID is retired"},
		{"retired resource claims null", "", "retired config fields present"},
		{"retired authz null", "", "retired config fields present"},
		{"retired environment empty", "", "retired environment override present"},
		{"invalid policy before migrations", "", "policy schema validation failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "retired leader election override" {
				t.Setenv("THV_REGISTRY_LEADER_ELECTION_ID", "old-controller")
			}
			if tc.name == "retired environment empty" {
				t.Setenv("THV_REGISTRY_AUTH_ROLES_SUPERADMIN", "")
			}
			dir := t.TempDir()
			policyPath := filepath.Join(dir, "policy.cedar")
			require.NoError(t, os.WriteFile(policyPath, []byte(`permit(principal,action == Registry::Action::"unknown",resource);`), 0600))
			configPath := filepath.Join(dir, "config.yaml")
			data := fmt.Sprintf(`sources:
 - name: managed
   managed: {}
registries:
 - name: default
   sources: [managed]
database:
 host: 127.0.0.1
 port: 1
 user: registry
 database: registry
 sslMode: disable
auth:
 mode: oauth
 oauth:
  providers:
   - name: test
     issuerUrl: https://id.example.org
     audience: registry-api
admin:
 enabled: true
authorization:
 policyFile: %s
 allowAnonymousCatalog: true
`, policyPath)
			switch tc.name {
			case "omitted auth defaults to OAuth", "omitted auth CLI override":
				before, after, _ := strings.Cut(data, "auth:\n")
				_, after, _ = strings.Cut(after, "admin:\n")
				data = before + "admin:\n" + after
			case "retired resource claims null":
				data = strings.Replace(data, "managed: {}", "managed: {}\n   claims: null", 1)
			case "retired authz null":
				data = strings.Replace(data, "auth:\n", "auth:\n authz: null\n", 1)
			}
			require.NoError(t, os.WriteFile(configPath, []byte(data), 0600))
			viper.Set("config", configPath)
			command := &cobra.Command{}
			command.SetContext(t.Context())
			command.Flags().String("auth-mode", tc.override, "")
			command.Flags().Bool("initialize-empty", true, "")
			require.ErrorContains(t, runServe(command, nil), tc.want)
		})
	}
}
